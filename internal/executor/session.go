package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/runkids/mdproof/internal/assertion"
	"github.com/runkids/mdproof/internal/core"
	"github.com/runkids/mdproof/internal/snapshot"
)

// ErrNotInContainer is returned when execution is attempted outside a container
// without the MDPROOF_ALLOW_EXECUTE override.
var ErrNotInContainer = fmt.Errorf(
	"mdproof: refusing to execute outside a container (strict mode)\n" +
		"  Runbook commands are designed to run inside a devcontainer/Docker.\n" +
		"  To run locally, use one of:\n" +
		"    mdproof sandbox <file>       auto-provision a container\n" +
		"    --strict=false               disable strict mode via CLI\n" +
		"    \"strict\": false              disable strict mode via mdproof.json\n" +
		"    MDPROOF_ALLOW_EXECUTE=1      environment variable override\n" +
		"    --dry-run                    parse without executing",
)

// IsContainerEnv returns true if we're running inside a Docker container
// or if the MDPROOF_ALLOW_EXECUTE env var is set.
func IsContainerEnv() bool {
	// Explicit override for testing or intentional host execution.
	if os.Getenv("MDPROOF_ALLOW_EXECUTE") != "" {
		return true
	}
	// Standard Docker marker file.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Podman / other container runtimes.
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	return false
}

// stepEndPattern matches the end-of-step marker emitted by the session script.
// Format: @@RB:END:<step_number>:<exit_code>:<duration_ms>@@
// Step number can be negative (synthetic setup/teardown steps use -1, -2).
var stepEndPattern = regexp.MustCompile(`^@@RB:END:(-?\d+):(-?\d+):(\d+)@@$`)

// newStepResult builds a StepResult with Source auto-populated from the step.
func newStepResult(step core.Step, status, errMsg string) core.StepResult {
	return core.StepResult{
		Step:   step,
		Source: core.StepSourceFromStep(step),
		Status: status,
		Error:  errMsg,
	}
}

// subBeginPattern matches the start-of-sub-command marker.
// Format: @@RB:SUB_BEGIN:<step_number>:<sub_index>@@
var subBeginPattern = regexp.MustCompile(`^@@RB:SUB_BEGIN:(-?\d+):(\d+)@@$`)

// subEndPattern matches the end-of-sub-command marker.
// Format: @@RB:SUB_END:<step_number>:<sub_index>:<exit_code>@@
var subEndPattern = regexp.MustCompile(`^@@RB:SUB_END:(-?\d+):(\d+):(-?\d+)@@$`)

// stepSetupPattern matches the step-setup completion marker.
// Format: @@RB:STEP_SETUP:<step_number>:<exit_code>@@
var stepSetupPattern = regexp.MustCompile(`^@@RB:STEP_SETUP:(-?\d+):(-?\d+)@@$`)

// stepTeardownPattern matches the step-teardown completion marker.
// Format: @@RB:STEP_TEARDOWN:<step_number>:<exit_code>@@
var stepTeardownPattern = regexp.MustCompile(`^@@RB:STEP_TEARDOWN:(-?\d+):(-?\d+)@@$`)

// indexedStep pairs a Step with its position in the original steps slice.
type indexedStep struct {
	idx  int
	step core.Step
}

// ParseSnapshotPattern checks if a pattern is a snapshot assertion.
// Returns (true, name) if it matches "snapshot: <name>" or "snapshot:<name>".
func ParseSnapshotPattern(pat string) (bool, string) {
	if strings.HasPrefix(pat, "snapshot:") {
		return true, strings.TrimSpace(pat[len("snapshot:"):])
	}
	return false, ""
}

func splitSnapshotExpected(expected []core.Expectation) (regular []core.Expectation, snapshots []core.Expectation) {
	for _, exp := range expected {
		if isSnap, _ := ParseSnapshotPattern(exp.Text); isSnap {
			snapshots = append(snapshots, exp)
		} else {
			regular = append(regular, exp)
		}
	}
	return
}

// SessionOptions controls session execution behavior.
type SessionOptions struct {
	Timeout             time.Duration
	FailFast            bool
	EnvVars             map[string]string
	SnapStore           *snapshot.Store
	RunbookName         string
	StepSetup           string // command to run before each step (empty = disabled)
	StepTeardown        string // command to run after each step (empty = disabled)
	KeepFailedArtifacts bool
}

// SessionRun carries the parsed results plus the temp artifact dir lifecycle.
type SessionRun struct {
	Results     []core.StepResult
	ArtifactDir string
	Cleanup     func()
}

type executionArtifacts struct {
	ScriptPath string
	EnvPath    string
	StdoutPath string
	StderrPath string
}

// ExecuteSession runs all auto steps in a single bash session, preserving
// shell variables across steps via an env file. Each step runs in a subshell
// with pipefail; an EXIT trap saves exported variables for the next step.
// The step's exit code is the exit code of the last command in the subshell.
func ExecuteSession(ctx context.Context, steps []core.Step, opts SessionOptions) []core.StepResult {
	run := ExecuteSessionDetailed(ctx, steps, opts)
	keep := opts.KeepFailedArtifacts && sessionHasFailure(run.Results)
	if !keep && run.Cleanup != nil {
		run.Cleanup()
	}
	return run.Results
}

// ExecuteSessionDetailed returns step results plus the temp artifact dir so
// callers can print or retain failure diagnostics before cleanup.
func ExecuteSessionDetailed(ctx context.Context, steps []core.Step, opts SessionOptions) SessionRun {
	if opts.Timeout == 0 {
		opts.Timeout = core.DefaultSessionTimeout
	}

	results := make([]core.StepResult, len(steps))

	// Defense in depth: refuse to execute outside a container.
	if !IsContainerEnv() {
		for i, s := range steps {
			results[i] = newStepResult(s, core.StatusFailed, ErrNotInContainer.Error())
		}
		return SessionRun{Results: results}
	}

	// Collect auto steps with their original indices.
	var autoSteps []indexedStep
	for i, s := range steps {
		if s.Executor == core.ExecutorAuto {
			autoSteps = append(autoSteps, indexedStep{idx: i, step: s})
		} else {
			results[i] = newStepResult(s, core.StatusSkipped, "")
		}
	}
	if len(autoSteps) == 0 {
		return SessionRun{Results: results}
	}

	// Create temp dir for stderr files and env persistence.
	tmpDir, err := os.MkdirTemp("", "mdproof-session-*")
	if err != nil {
		for _, as := range autoSteps {
			results[as.idx] = newStepResult(as.step, core.StatusFailed, fmt.Sprintf("create temp dir: %v", err))
		}
		return SessionRun{Results: results}
	}

	if err := writeArtifactScripts(autoSteps, tmpDir, opts); err != nil {
		for _, as := range autoSteps {
			results[as.idx] = newStepResult(as.step, core.StatusFailed, fmt.Sprintf("write artifact scripts: %v", err))
		}
		_ = os.RemoveAll(tmpDir)
		return SessionRun{Results: results}
	}

	script := buildSessionScript(autoSteps, tmpDir, opts)

	scriptFile := filepath.Join(tmpDir, "session.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		for _, as := range autoSteps {
			results[as.idx] = newStepResult(as.step, core.StatusFailed, fmt.Sprintf("write script: %v", err))
		}
		_ = os.RemoveAll(tmpDir)
		return SessionRun{Results: results}
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", scriptFile)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil // step-level stderr goes to temp files

	_ = cmd.Run() // exit code is per-step, not global

	parseSessionResults(&stdout, autoSteps, results, tmpDir)

	// Run assertions on each completed step.
	// If assertions are defined, they always run (regardless of exit code)
	// and determine the final pass/fail status.
	// Layer 2 fail-fast: if a step fails after assertions, skip remaining steps.
	failFastTriggered := false
	for _, as := range autoSteps {
		r := &results[as.idx]
		if opts.FailFast && failFastTriggered && r.Status != core.StatusSkipped {
			r.Status = core.StatusSkipped
			r.Error = "skipped: earlier step failed (--fail-fast)"
			continue
		}
		if r.Status == core.StatusPassed || r.Status == core.StatusFailed {
			regularExpected, snapshotExpected := splitSnapshotExpected(as.step.Expected)
			regularStep := as.step
			regularStep.Expected = regularExpected

			assertion.CheckStep(r, regularStep)

			if opts.SnapStore != nil && len(snapshotExpected) > 0 {
				for _, exp := range snapshotExpected {
					_, name := ParseSnapshotPattern(exp.Text)
					snapResult := opts.SnapStore.Check(name, r.Stdout, opts.RunbookName)
					if !exp.Source.IsZero() {
						source := exp.Source
						snapResult.Source = &source
					}
					r.Assertions = append(r.Assertions, snapResult)
					if !snapResult.Matched {
						r.Status = core.StatusFailed
					}
				}
			}
		}
		if opts.FailFast && r.Status == core.StatusFailed {
			failFastTriggered = true
		}
	}

	populateDebugMetadata(results, autoSteps, tmpDir)

	return SessionRun{
		Results:     results,
		ArtifactDir: tmpDir,
		Cleanup: func() {
			_ = os.RemoveAll(tmpDir)
		},
	}
}

func sessionHasFailure(results []core.StepResult) bool {
	for _, r := range results {
		if r.Status == core.StatusFailed {
			return true
		}
	}
	return false
}

func splitStepSubCommands(command string) []string {
	parts := strings.Split(command, "\n---\n")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func stepArtifacts(tmpDir string, stepNum int) executionArtifacts {
	base := filepath.Join(tmpDir, fmt.Sprintf("step_%d", stepNum))
	return executionArtifacts{
		ScriptPath: base + ".sh",
		EnvPath:    base + "_env",
		StdoutPath: base + "_out",
		StderrPath: base + "_err",
	}
}

func hookArtifacts(tmpDir string, stepNum int, hook string) executionArtifacts {
	base := filepath.Join(tmpDir, fmt.Sprintf("step_%d_%s", stepNum, hook))
	return executionArtifacts{
		ScriptPath: base + ".sh",
		EnvPath:    base + "_env",
		StdoutPath: base + "_out",
		StderrPath: base + "_err",
	}
}

func subArtifacts(tmpDir string, stepNum, subIdx int) executionArtifacts {
	base := filepath.Join(tmpDir, fmt.Sprintf("step_%d_sub_%d", stepNum, subIdx))
	return executionArtifacts{
		ScriptPath: base + ".sh",
		EnvPath:    base + "_env",
		StdoutPath: base + "_out",
		StderrPath: base + "_err",
	}
}

func renderStepScript(command string) string {
	parts := splitStepSubCommands(command)
	if len(parts) <= 1 {
		if strings.HasSuffix(command, "\n") {
			return command
		}
		return command + "\n"
	}

	var sb strings.Builder
	for i, part := range parts {
		if i > 0 {
			sb.WriteString("\n# --- mdproof sub-command ---\n")
		}
		sb.WriteString(part)
		if !strings.HasSuffix(part, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func writeArtifactScript(path, content string) error {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0700)
}

func writeArtifactScripts(autoSteps []indexedStep, tmpDir string, opts SessionOptions) error {
	for _, as := range autoSteps {
		if err := writeArtifactScript(stepArtifacts(tmpDir, as.step.Number).ScriptPath, renderStepScript(as.step.Command)); err != nil {
			return err
		}
		if opts.StepSetup != "" {
			if err := writeArtifactScript(hookArtifacts(tmpDir, as.step.Number, "setup").ScriptPath, opts.StepSetup); err != nil {
				return err
			}
		}
		if opts.StepTeardown != "" {
			if err := writeArtifactScript(hookArtifacts(tmpDir, as.step.Number, "teardown").ScriptPath, opts.StepTeardown); err != nil {
				return err
			}
		}
		for i, sub := range splitStepSubCommands(as.step.Command) {
			if len(splitStepSubCommands(as.step.Command)) <= 1 {
				break
			}
			if err := writeArtifactScript(subArtifacts(tmpDir, as.step.Number, i).ScriptPath, sub); err != nil {
				return err
			}
		}
	}
	return nil
}

func readDebugEnvironment(path string) *core.DebugEnvironment {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	env := &core.DebugEnvironment{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "PWD":
			env.PWD = val
		case "HOME":
			env.HOME = val
		case "TMPDIR":
			env.TMPDIR = val
		}
	}
	if env.PWD == "" && env.HOME == "" && env.TMPDIR == "" {
		return nil
	}
	return env
}

func newStepDebug(art executionArtifacts) *core.StepDebug {
	return &core.StepDebug{
		ScriptPath:  art.ScriptPath,
		EnvPath:     art.EnvPath,
		StdoutPath:  art.StdoutPath,
		StderrPath:  art.StderrPath,
		Environment: readDebugEnvironment(art.EnvPath),
	}
}

func hasFailingAssertion(r core.StepResult) bool {
	for _, a := range r.Assertions {
		if !a.Matched {
			return true
		}
	}
	return false
}

func firstFailingSubCommand(r core.StepResult) int {
	for i, sc := range r.SubCommands {
		if sc.ExitCode != 0 {
			return i
		}
	}
	return -1
}

func populateDebugMetadata(results []core.StepResult, autoSteps []indexedStep, tmpDir string) {
	for _, as := range autoSteps {
		r := &results[as.idx]
		if r.StepSetup != nil && r.StepSetup.ExitCode != 0 {
			r.Debug = newStepDebug(hookArtifacts(tmpDir, as.step.Number, "setup"))
			continue
		}
		if r.Status != core.StatusFailed {
			continue
		}
		if r.ExitCode == 0 && hasFailingAssertion(*r) {
			r.Debug = newStepDebug(stepArtifacts(tmpDir, as.step.Number))
			continue
		}
		if subIdx := firstFailingSubCommand(*r); subIdx >= 0 {
			r.Debug = newStepDebug(subArtifacts(tmpDir, as.step.Number, subIdx))
			continue
		}
		if r.ExitCode != 0 {
			r.Debug = newStepDebug(stepArtifacts(tmpDir, as.step.Number))
		}
	}
}

// buildSessionScript generates a single bash script that executes all steps
// sequentially, emitting markers to stdout for per-step output parsing.
// When opts.FailFast is true, a step failure sets __rb_stop=1 and subsequent steps
// emit skip markers (exit code -1) without executing.
func buildSessionScript(steps []indexedStep, tmpDir string, opts SessionOptions) string {
	envFile := filepath.Join(tmpDir, "env")

	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -o pipefail\n\n")

	// Seed environment variables from config.
	if len(opts.EnvVars) > 0 {
		keys := core.SortedKeys(opts.EnvVars)
		for _, k := range keys {
			fmt.Fprintf(&sb, "export %s=%q\n", k, opts.EnvVars[k])
		}
		sb.WriteByte('\n')
	}

	// Timing helper: works on Linux (date +%s%N) and macOS (date +%s).
	sb.WriteString("__rb_now_ms() {\n")
	sb.WriteString("  local ns\n")
	sb.WriteString("  ns=$(date +%s%N 2>/dev/null)\n")
	sb.WriteString("  if [ ${#ns} -gt 10 ]; then\n")
	sb.WriteString("    echo $(( ns / 1000000 ))\n")
	sb.WriteString("  else\n")
	sb.WriteString("    echo $(( $(date +%s) * 1000 ))\n")
	sb.WriteString("  fi\n")
	sb.WriteString("}\n\n")

	sb.WriteString("__rb_capture_env() {\n")
	sb.WriteString("  printf 'PWD=%s\\nHOME=%s\\nTMPDIR=%s\\n' \"$PWD\" \"$HOME\" \"$TMPDIR\"\n")
	sb.WriteString("}\n\n")

	if opts.FailFast {
		sb.WriteString("__rb_stop=0\n\n")
	}

	for _, as := range steps {
		n := as.step.Number

		command := as.step.Command
		filtered := splitStepSubCommands(command)
		isMultiSub := len(filtered) > 1

		fmt.Fprintf(&sb, "# Step %d: %s\n", n, as.step.Title)

		if opts.FailFast {
			sb.WriteString("if [ \"$__rb_stop\" = \"0\" ]; then\n")
		}

		// depends directive: skip if the depended-on step failed.
		if as.step.DependsOn > 0 {
			fmt.Fprintf(&sb, "if [ \"${__rb_status_%d:-1}\" = \"0\" ]; then\n", as.step.DependsOn)
		}

		hasSetup := opts.StepSetup != ""
		hasTeardown := opts.StepTeardown != ""
		hasRetry := as.step.Retry > 0
		retryWrapsHooks := hasRetry && (hasSetup || hasTeardown)

		if retryWrapsHooks {
			// Path A: retry wraps setup + body + teardown.
			// BEGIN and timing BEFORE retry loop.
			fmt.Fprintf(&sb, "echo '@@RB:BEGIN:%d@@'\n", n)
			sb.WriteString("__rb_t0=$(__rb_now_ms)\n")

			attempts := as.step.Retry + 1
			fmt.Fprintf(&sb, "for __rb_attempt in $(seq 1 %d); do\n", attempts)

			// Setup inside retry loop.
			if hasSetup {
				art := hookArtifacts(tmpDir, n, "setup")
				fmt.Fprintf(&sb, "(\n  set -o pipefail\n  [ -f %q ] && source %q\n  __rb_capture_env > %q\n  %s\n) >%q 2>%q\n",
					envFile, envFile, art.EnvPath, opts.StepSetup, art.StdoutPath, art.StderrPath)
				sb.WriteString("__rb_setup_rc=$?\n")
				fmt.Fprintf(&sb, "echo \"@@RB:STEP_SETUP:%d:${__rb_setup_rc}@@\"\n", n)
				sb.WriteString("if [ $__rb_setup_rc -ne 0 ]; then\n")
				sb.WriteString("  __rb_rc=$__rb_setup_rc\n")
				sb.WriteString("else\n")
			}

			// Body.
			if isMultiSub {
				sb.WriteString(buildSubCommandSubshells(as.step, filtered, envFile, tmpDir, opts.FailFast))
			} else {
				singleCmd := strings.ReplaceAll(command, "\n---\n", "\n")
				subshell := buildStepSubshell(as.step, singleCmd, envFile, stepArtifacts(tmpDir, n))
				sb.WriteString(subshell)
				sb.WriteString("__rb_rc=$?\n")
				fmt.Fprintf(&sb, "cat %q\n", stepArtifacts(tmpDir, n).StdoutPath)
			}

			if hasSetup {
				sb.WriteString("fi\n")
			}

			// Teardown inside retry loop.
			if hasTeardown {
				art := hookArtifacts(tmpDir, n, "teardown")
				fmt.Fprintf(&sb, "(\n  set -o pipefail\n  [ -f %q ] && source %q\n  __rb_capture_env > %q\n  %s\n) >%q 2>%q\n",
					envFile, envFile, art.EnvPath, opts.StepTeardown, art.StdoutPath, art.StderrPath)
				sb.WriteString("__rb_teardown_rc=$?\n")
				fmt.Fprintf(&sb, "echo \"@@RB:STEP_TEARDOWN:%d:${__rb_teardown_rc}@@\"\n", n)
			}

			sb.WriteString("[ $__rb_rc -eq 0 ] && break\n")
			if as.step.RetryDelay > 0 {
				fmt.Fprintf(&sb, "[ $__rb_attempt -lt %d ] && sleep %d\n", attempts, int(as.step.RetryDelay.Seconds()))
			}
			sb.WriteString("done\n")

			sb.WriteString("__rb_t1=$(__rb_now_ms)\n")
			sb.WriteString("__rb_dur=$(( __rb_t1 - __rb_t0 ))\n")
			fmt.Fprintf(&sb, "echo \"@@RB:END:%d:${__rb_rc}:${__rb_dur}@@\"\n", n)
			fmt.Fprintf(&sb, "__rb_status_%d=$__rb_rc\n", n)
		} else {
			// Path B: current behavior (no retry-hooks interaction).

			// Step-setup: runs before step body, captures output to temp files.
			if hasSetup {
				art := hookArtifacts(tmpDir, n, "setup")
				fmt.Fprintf(&sb, "(\n  set -o pipefail\n  [ -f %q ] && source %q\n  __rb_capture_env > %q\n  %s\n) >%q 2>%q\n",
					envFile, envFile, art.EnvPath, opts.StepSetup, art.StdoutPath, art.StderrPath)
				sb.WriteString("__rb_setup_rc=$?\n")
				fmt.Fprintf(&sb, "echo \"@@RB:STEP_SETUP:%d:${__rb_setup_rc}@@\"\n", n)
				sb.WriteString("if [ $__rb_setup_rc -ne 0 ]; then\n")
				// Setup failed: emit failed step marker, skip step body.
				fmt.Fprintf(&sb, "  echo '@@RB:BEGIN:%d@@'\n", n)
				fmt.Fprintf(&sb, "  echo \"@@RB:END:%d:${__rb_setup_rc}:0@@\"\n", n)
				fmt.Fprintf(&sb, "  __rb_status_%d=$__rb_setup_rc\n", n)
				sb.WriteString("  __rb_rc=$__rb_setup_rc\n")
				if opts.FailFast {
					sb.WriteString("  __rb_stop=1\n")
				}
				sb.WriteString("else\n")
			}

			fmt.Fprintf(&sb, "echo '@@RB:BEGIN:%d@@'\n", n)
			sb.WriteString("__rb_t0=$(__rb_now_ms)\n")

			if isMultiSub {
				// Multi sub-command: each gets its own subshell.
				if hasRetry {
					attempts := as.step.Retry + 1
					fmt.Fprintf(&sb, "for __rb_attempt in $(seq 1 %d); do\n", attempts)
					sb.WriteString(buildSubCommandSubshells(as.step, filtered, envFile, tmpDir, opts.FailFast))
					sb.WriteString("[ $__rb_rc -eq 0 ] && break\n")
					if as.step.RetryDelay > 0 {
						fmt.Fprintf(&sb, "[ $__rb_attempt -lt %d ] && sleep %d\n", attempts, int(as.step.RetryDelay.Seconds()))
					}
					sb.WriteString("done\n")
				} else {
					sb.WriteString(buildSubCommandSubshells(as.step, filtered, envFile, tmpDir, opts.FailFast))
				}
			} else {
				// Single command: use existing subshell (no behavior change).
				singleCmd := command
				singleCmd = strings.ReplaceAll(singleCmd, "\n---\n", "\n")
				art := stepArtifacts(tmpDir, n)
				subshell := buildStepSubshell(as.step, singleCmd, envFile, art)

				// retry directive: wrap subshell in a for loop.
				if hasRetry {
					attempts := as.step.Retry + 1
					fmt.Fprintf(&sb, "for __rb_attempt in $(seq 1 %d); do\n", attempts)
					sb.WriteString(subshell)
					sb.WriteString("__rb_rc=$?\n")
					fmt.Fprintf(&sb, "cat %q\n", art.StdoutPath)
					sb.WriteString("[ $__rb_rc -eq 0 ] && break\n")
					if as.step.RetryDelay > 0 {
						fmt.Fprintf(&sb, "[ $__rb_attempt -lt %d ] && sleep %d\n", attempts, int(as.step.RetryDelay.Seconds()))
					}
					sb.WriteString("done\n")
				} else {
					sb.WriteString(subshell)
					sb.WriteString("__rb_rc=$?\n")
					fmt.Fprintf(&sb, "cat %q\n", art.StdoutPath)
				}
			}

			sb.WriteString("__rb_t1=$(__rb_now_ms)\n")
			sb.WriteString("__rb_dur=$(( __rb_t1 - __rb_t0 ))\n")
			fmt.Fprintf(&sb, "echo \"@@RB:END:%d:${__rb_rc}:${__rb_dur}@@\"\n", n)

			// Track step status for depends directives.
			fmt.Fprintf(&sb, "__rb_status_%d=$__rb_rc\n", n)

			// Close step-setup if-block.
			if hasSetup {
				sb.WriteString("fi\n") // close the setup check
			}
		}

		// Close depends block.
		if as.step.DependsOn > 0 {
			sb.WriteString("else\n")
			fmt.Fprintf(&sb, "echo '@@RB:END:%d:%d:0@@'\n", n, core.ExitCodeDependsSkipped)
			fmt.Fprintf(&sb, "__rb_status_%d=%d\n", n, core.ExitCodeDependsSkipped)
			sb.WriteString("__rb_rc=0\n") // prevent stale __rb_rc from triggering fail-fast
			sb.WriteString("fi\n")
		}

		if opts.FailFast {
			sb.WriteString("[ $__rb_rc -ne 0 ] && __rb_stop=1\n")
			sb.WriteString("else\n")
			// Emit skip marker for skipped.
			fmt.Fprintf(&sb, "echo '@@RB:END:%d:%d:0@@'\n", n, core.ExitCodeFailFastSkipped)
			sb.WriteString("fi\n")
		}

		// Step-teardown: runs after step body, even if step failed.
		// Only runs if step actually executed (not fail-fast-skipped).
		// Skipped when retryWrapsHooks is true (teardown already handled inside retry loop).
		if hasTeardown && !retryWrapsHooks {
			if opts.FailFast {
				// Only run teardown if this step was not skipped by fail-fast.
				fmt.Fprintf(&sb, "if [ \"${__rb_status_%d+set}\" = \"set\" ]; then\n", n)
			}
			art := hookArtifacts(tmpDir, n, "teardown")
			fmt.Fprintf(&sb, "(\n  set -o pipefail\n  [ -f %q ] && source %q\n  __rb_capture_env > %q\n  %s\n) >%q 2>%q\n",
				envFile, envFile, art.EnvPath, opts.StepTeardown, art.StdoutPath, art.StderrPath)
			sb.WriteString("__rb_teardown_rc=$?\n")
			fmt.Fprintf(&sb, "echo \"@@RB:STEP_TEARDOWN:%d:${__rb_teardown_rc}@@\"\n", n)
			if opts.FailFast {
				sb.WriteString("fi\n")
			}
		}

		sb.WriteByte('\n')
	}

	return sb.String()
}

// buildStepSubshell generates the subshell block for a single step.
// Returns the subshell string WITHOUT the trailing `__rb_rc=$?`.
func buildStepSubshell(step core.Step, command, envFile string, art executionArtifacts) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "(\n")
	fmt.Fprintf(&sb, "  set -o pipefail -a\n")
	fmt.Fprintf(&sb, "  [ -f %q ] && source %q\n", envFile, envFile)
	fmt.Fprintf(&sb, "  __rb_capture_env > %q\n", art.EnvPath)
	fmt.Fprintf(&sb, "  __rb_save_env() { export -p > %q 2>/dev/null; }\n", envFile)
	fmt.Fprintf(&sb, "  trap __rb_save_env EXIT\n")

	if step.Timeout > 0 {
		secs := int(step.Timeout.Seconds())
		if secs < 1 {
			secs = 1
		}
		fmt.Fprintf(&sb, "  timeout %d bash <<'__RB_STEP_%d__'\n", secs, step.Number)
		fmt.Fprintf(&sb, "%s\n", command)
		fmt.Fprintf(&sb, "__RB_STEP_%d__\n", step.Number)
	} else {
		fmt.Fprintf(&sb, "  %s\n", command)
	}

	fmt.Fprintf(&sb, ") >%q 2>%q\n", art.StdoutPath, art.StderrPath)
	return sb.String()
}

// buildSubCommandSubshells generates separate subshell blocks for each sub-command
// within a step. Each sub-command runs in its own (...) subshell with independent
// stdout/stderr capture. Every sub-command saves the environment for the next.
func buildSubCommandSubshells(step core.Step, subCommands []string, envFile, tmpDir string, failFast bool) string {
	var sb strings.Builder
	sb.WriteString("__rb_rc=0\n")
	for i, sub := range subCommands {
		art := subArtifacts(tmpDir, step.Number, i)
		if failFast && i > 0 {
			sb.WriteString("if [ $__rb_rc -eq 0 ]; then\n")
		}
		fmt.Fprintf(&sb, "echo '@@RB:SUB_BEGIN:%d:%d@@'\n", step.Number, i)
		fmt.Fprintf(&sb, "(\n")
		fmt.Fprintf(&sb, "  set -o pipefail -a\n")
		fmt.Fprintf(&sb, "  [ -f %q ] && source %q\n", envFile, envFile)
		fmt.Fprintf(&sb, "  __rb_capture_env > %q\n", art.EnvPath)
		fmt.Fprintf(&sb, "  __rb_save_env() { export -p > %q 2>/dev/null; }\n", envFile)
		fmt.Fprintf(&sb, "  trap __rb_save_env EXIT\n")
		fmt.Fprintf(&sb, "  %s\n", sub)
		fmt.Fprintf(&sb, ") >%q 2>%q\n", art.StdoutPath, art.StderrPath)
		sb.WriteString("__rb_sub_rc=$?\n")
		fmt.Fprintf(&sb, "cat %q\n", art.StdoutPath)
		sb.WriteString("[ $__rb_rc -eq 0 ] && __rb_rc=$__rb_sub_rc\n")
		fmt.Fprintf(&sb, "echo \"@@RB:SUB_END:%d:%d:${__rb_sub_rc}@@\"\n", step.Number, i)
		if failFast && i > 0 {
			sb.WriteString("fi\n")
		}
	}
	return sb.String()
}

// parseSessionResults reads the combined stdout and splits it into per-step
// results using the @@RB:BEGIN/END markers.
func parseSessionResults(stdout *bytes.Buffer, autoSteps []indexedStep, results []core.StepResult, tmpDir string) {
	// Build a map from step number to autoSteps index.
	stepMap := make(map[int]int, len(autoSteps))
	for i, as := range autoSteps {
		stepMap[as.step.Number] = i
	}

	// Pre-compute sub-command texts per step for report population.
	subCmdTexts := make(map[int][]string) // step number -> sub-command texts
	for _, as := range autoSteps {
		parts := strings.Split(as.step.Command, "\n---\n")
		var filtered []string
		for _, sc := range parts {
			sc = strings.TrimSpace(sc)
			if sc != "" {
				filtered = append(filtered, sc)
			}
		}
		if len(filtered) > 1 {
			subCmdTexts[as.step.Number] = filtered
		}
	}

	scanner := bufio.NewScanner(stdout)
	var currentBuf strings.Builder
	var currentSubBuf strings.Builder
	inStep := false
	inSub := false

	// Track sub-command results for the current step.
	var pendingSubCmds []core.SubCommandResult

	for scanner.Scan() {
		line := scanner.Text()

		// Step-setup marker: appears before @@RB:BEGIN@@.
		if m := stepSetupPattern.FindStringSubmatch(line); m != nil {
			stepNum, _ := strconv.Atoi(m[1])
			exitCode, _ := strconv.Atoi(m[2])
			if asIdx, ok := stepMap[stepNum]; ok {
				r := &results[autoSteps[asIdx].idx]
				outFile := filepath.Join(tmpDir, fmt.Sprintf("step_%d_setup_out", stepNum))
				errFile := filepath.Join(tmpDir, fmt.Sprintf("step_%d_setup_err", stepNum))
				setupOut, _ := os.ReadFile(outFile)
				setupErr, _ := os.ReadFile(errFile)
				r.StepSetup = &core.HookExecResult{
					ExitCode: exitCode,
					Stdout:   strings.TrimSpace(string(setupOut)),
					Stderr:   strings.TrimSpace(string(setupErr)),
				}
				if exitCode != 0 {
					r.Error = fmt.Sprintf("step-setup failed: %s", strings.TrimSpace(string(setupErr)))
				}
			}
			continue
		}

		// Step-teardown marker: appears after @@RB:END@@.
		if m := stepTeardownPattern.FindStringSubmatch(line); m != nil {
			stepNum, _ := strconv.Atoi(m[1])
			exitCode, _ := strconv.Atoi(m[2])
			if asIdx, ok := stepMap[stepNum]; ok {
				r := &results[autoSteps[asIdx].idx]
				outFile := filepath.Join(tmpDir, fmt.Sprintf("step_%d_teardown_out", stepNum))
				errFile := filepath.Join(tmpDir, fmt.Sprintf("step_%d_teardown_err", stepNum))
				tdOut, _ := os.ReadFile(outFile)
				tdErr, _ := os.ReadFile(errFile)
				r.StepTeardown = &core.HookExecResult{
					ExitCode: exitCode,
					Stdout:   strings.TrimSpace(string(tdOut)),
					Stderr:   strings.TrimSpace(string(tdErr)),
				}
			}
			continue
		}

		if strings.HasPrefix(line, "@@RB:BEGIN:") && strings.HasSuffix(line, "@@") {
			numStr := line[len("@@RB:BEGIN:") : len(line)-2]
			if _, err := strconv.Atoi(numStr); err == nil {
				currentBuf.Reset()
				currentSubBuf.Reset()
				pendingSubCmds = nil
				inStep = true
				inSub = false
			}
			continue
		}

		if m := subBeginPattern.FindStringSubmatch(line); m != nil {
			currentSubBuf.Reset()
			inSub = true
			continue
		}

		if m := subEndPattern.FindStringSubmatch(line); m != nil {
			stepNum, _ := strconv.Atoi(m[1])
			subIdx, _ := strconv.Atoi(m[2])
			subExitCode, _ := strconv.Atoi(m[3])

			subStdout := currentSubBuf.String()

			// Append sub-command stdout to step-level combined stdout.
			if currentBuf.Len() > 0 && subStdout != "" {
				currentBuf.WriteByte('\n')
			}
			currentBuf.WriteString(subStdout)

			// Read sub-command stderr from temp file.
			var subStderr string
			subErrFile := filepath.Join(tmpDir, fmt.Sprintf("step_%d_sub_%d_err", stepNum, subIdx))
			if data, err := os.ReadFile(subErrFile); err == nil {
				subStderr = strings.TrimSpace(string(data))
			}

			// Determine sub-command text.
			cmdText := ""
			if texts, ok := subCmdTexts[stepNum]; ok && subIdx < len(texts) {
				cmdText = texts[subIdx]
			}

			pendingSubCmds = append(pendingSubCmds, core.SubCommandResult{
				Command:  cmdText,
				ExitCode: subExitCode,
				Stdout:   subStdout,
				Stderr:   subStderr,
			})

			currentSubBuf.Reset()
			inSub = false
			continue
		}

		if m := stepEndPattern.FindStringSubmatch(line); m != nil {
			stepNum, _ := strconv.Atoi(m[1])
			exitCode, _ := strconv.Atoi(m[2])
			durationMs, _ := strconv.ParseInt(m[3], 10, 64)

			if asIdx, ok := stepMap[stepNum]; ok {
				as := autoSteps[asIdx]
				r := &results[as.idx]
				r.Step = as.step
				r.Source = core.StepSourceFromStep(as.step)

				// Exit code sentinels for skipped steps.
				if exitCode == core.ExitCodeFailFastSkipped {
					r.Status = core.StatusSkipped
					r.Error = "skipped: earlier step failed (--fail-fast)"
				} else if exitCode == core.ExitCodeDependsSkipped {
					r.Status = core.StatusSkipped
					r.Error = fmt.Sprintf("skipped: depends on step %d (failed)", as.step.DependsOn)
				} else {
					r.ExitCode = exitCode
					r.DurationMs = durationMs
					r.Stdout = currentBuf.String()

					// For single-command steps, read step-level stderr.
					// For multi sub-command steps, combine sub-command stderr.
					if len(pendingSubCmds) > 0 {
						r.SubCommands = pendingSubCmds
						var combinedStderr strings.Builder
						for _, sc := range pendingSubCmds {
							if sc.Stderr != "" {
								if combinedStderr.Len() > 0 {
									combinedStderr.WriteByte('\n')
								}
								combinedStderr.WriteString(sc.Stderr)
							}
						}
						r.Stderr = combinedStderr.String()
					} else {
						errFile := filepath.Join(tmpDir, fmt.Sprintf("step_%d_err", stepNum))
						if data, err := os.ReadFile(errFile); err == nil {
							r.Stderr = string(data)
						}
					}

					if exitCode == 0 {
						r.Status = core.StatusPassed
					} else {
						r.Status = core.StatusFailed
					}
				}
			}
			inStep = false
			inSub = false
			pendingSubCmds = nil
			continue
		}

		if inSub {
			if currentSubBuf.Len() > 0 {
				currentSubBuf.WriteByte('\n')
			}
			currentSubBuf.WriteString(line)
		} else if inStep {
			if currentBuf.Len() > 0 {
				currentBuf.WriteByte('\n')
			}
			currentBuf.WriteString(line)
		}
	}

	// Mark any auto steps without results as failed (e.g., script aborted).
	for _, as := range autoSteps {
		if results[as.idx].Status == "" {
			results[as.idx] = newStepResult(as.step, core.StatusFailed, "step did not complete (session aborted)")
		}
	}
}
