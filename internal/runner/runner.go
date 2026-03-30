package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/runkids/mdproof/internal/core"
	"github.com/runkids/mdproof/internal/executor"
	"github.com/runkids/mdproof/internal/parser"
	"github.com/runkids/mdproof/internal/report"
	"github.com/runkids/mdproof/internal/snapshot"
)

// RunOptions controls runner behavior.
type RunOptions struct {
	DryRun              bool
	JSONOutput          io.Writer
	Timeout             time.Duration
	SourcePath          string            // full path used for source-aware parse/reporting
	Setup               string            // command to run before the runbook
	Teardown            string            // command to run after the runbook
	Steps               []int             // only run these step numbers (empty = all)
	From                int               // run from this step number onwards (0 = disabled)
	FailFast            bool              // stop after first failed step
	Env                 map[string]string // environment variables seeded into all steps
	SnapshotUpdate      bool              // --update-snapshots: overwrite snapshots instead of comparing
	RunbookDir          string            // base directory for snapshot storage
	Inline              bool              // parse with inline markers instead of step headings
	StepSetup           string            // command to run before each step
	StepTeardown        string            // command to run after each step
	KeepFailedArtifacts bool              // retain failure artifacts for CLI debugging
	PrintStepScript     bool              // print the failed execution unit script to stderr
	PrintStepEnv        bool              // print the failed execution unit env snapshot to stderr
}

// shouldRun reports whether stepNum should execute given the filter flags.
func (o RunOptions) shouldRun(stepNum int) bool {
	if len(o.Steps) > 0 {
		return slices.Contains(o.Steps, stepNum)
	}
	if o.From > 0 {
		return stepNum >= o.From
	}
	return true
}

// HookResult holds the outcome of a build hook execution.
type HookResult struct {
	OK       bool
	ExitCode int
	Output   string
	Duration time.Duration
}

// RunResult carries the report plus artifact lifecycle details for CLI callers.
type RunResult struct {
	Report      core.Report
	ArtifactDir string
	Cleanup     func()
}

// RunBuildHook executes the build command as a simple shell process.
// Unlike setup/teardown which run inside the session executor,
// build runs once before any runbook and uses os/exec directly.
func RunBuildHook(command string) *HookResult {
	start := time.Now()

	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = os.Stderr // show build output to stderr so it doesn't mix with JSON
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Fprintf(os.Stderr, "  Build: running...\n")

	err := cmd.Run()
	dur := time.Since(start)

	result := &HookResult{
		OK:       err == nil,
		Duration: dur,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
			result.Output = err.Error()
		}
	}

	if result.OK {
		fmt.Fprintf(os.Stderr, "  Build: passed (%.1fs)\n", dur.Seconds())
	} else {
		fmt.Fprintf(os.Stderr, "  Build: failed (%.1fs)\n", dur.Seconds())
	}

	return result
}

// ResolveFiles finds runbook/proof files from a path (file or directory).
// When given a directory, it looks for files matching *_runbook.md, *-runbook.md,
// *_proof.md, or *-proof.md.
func ResolveFiles(target string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{target}, nil
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, "_runbook.md") ||
			strings.HasSuffix(name, "-runbook.md") ||
			strings.HasSuffix(name, "_proof.md") ||
			strings.HasSuffix(name, "-proof.md") {
			files = append(files, filepath.Join(target, name))
		}
	}
	return files, nil
}

// Run parses, classifies, executes, and reports a runbook.
// Non-dry-run execution uses a session executor that preserves shell
// variables across steps (single bash process with env file persistence).
func Run(r io.Reader, name string, opts RunOptions) (core.Report, error) {
	result, err := RunDetailed(r, name, opts)
	if err != nil {
		return core.Report{}, err
	}

	keep := opts.KeepFailedArtifacts && reportHasRetainableFailure(result.Report)
	if keep {
		result.Report.ArtifactDir = result.ArtifactDir
	} else if result.Cleanup != nil {
		result.Cleanup()
	}

	if opts.JSONOutput != nil {
		if err := report.WriteJSONReport(opts.JSONOutput, result.Report); err != nil {
			return result.Report, err
		}
	}

	return result.Report, nil
}

// RunDetailed returns the parsed report plus any temp artifact dir so callers
// can print or retain diagnostics before cleanup.
func RunDetailed(r io.Reader, name string, opts RunOptions) (RunResult, error) {
	if opts.Timeout == 0 {
		opts.Timeout = core.DefaultStepTimeout
	}

	var rb *core.Runbook
	var err error
	sourcePath := opts.SourcePath
	if sourcePath == "" {
		sourcePath = name
	}
	if opts.Inline {
		rb, err = parser.ParseInline(r, sourcePath)
	} else {
		rb, err = parser.ParseRunbookFile(r, sourcePath)
	}
	if err != nil {
		return RunResult{}, err
	}

	steps := parser.ClassifyAll(rb.Steps)

	// Snapshot validation: check for duplicates and track whether any exist.
	seenSnaps := make(map[string]bool)
	hasSnapshots := false
	for _, s := range steps {
		for _, exp := range s.Expected {
			if isSnap, snapName := executor.ParseSnapshotPattern(exp.Text); isSnap {
				if seenSnaps[snapName] {
					return RunResult{}, fmt.Errorf("duplicate snapshot name %q in runbook", snapName)
				}
				seenSnaps[snapName] = true
				hasSnapshots = true
			}
		}
	}

	// Apply step filter: mark filtered-out auto steps as manual so they get skipped.
	for i, s := range steps {
		if s.Executor == core.ExecutorAuto && !opts.shouldRun(s.Number) {
			steps[i].Executor = core.ExecutorManual
		}
	}

	start := time.Now()
	var results []core.StepResult
	var setupResult, teardownResult *core.StepResult
	var artifactDir string
	var cleanup func()

	if opts.DryRun {
		// Dry-run: skip all steps.
		results = make([]core.StepResult, len(steps))
		for i, s := range steps {
			results[i] = core.StepResult{Step: s, Source: core.StepSourceFromStep(s), Status: core.StatusSkipped}
		}
	} else {
		// Inject setup/teardown as synthetic steps.
		execSteps := steps
		setupIdx, teardownIdx := -1, -1
		if opts.Setup != "" {
			setupStep := core.Step{
				Number:   -1,
				Title:    "[setup]",
				Command:  opts.Setup,
				Lang:     "bash",
				Executor: core.ExecutorAuto,
			}
			setupIdx = 0
			execSteps = append([]core.Step{setupStep}, execSteps...)
		}
		if opts.Teardown != "" {
			teardownStep := core.Step{
				Number:   -2,
				Title:    "[teardown]",
				Command:  opts.Teardown,
				Lang:     "bash",
				Executor: core.ExecutorAuto,
			}
			teardownIdx = len(execSteps)
			execSteps = append(execSteps, teardownStep)
		}

		var snapStore *snapshot.Store
		if hasSnapshots {
			snapStore = snapshot.NewStore(opts.RunbookDir, opts.SnapshotUpdate)
		}

		sessionRun := executor.ExecuteSessionDetailed(context.Background(), execSteps, executor.SessionOptions{
			Timeout:      opts.Timeout,
			FailFast:     opts.FailFast,
			EnvVars:      opts.Env,
			SnapStore:    snapStore,
			RunbookName:  name,
			StepSetup:    opts.StepSetup,
			StepTeardown: opts.StepTeardown,
		})
		allResults := sessionRun.Results
		artifactDir = sessionRun.ArtifactDir
		cleanup = sessionRun.Cleanup

		// Extract setup result — if setup failed, mark all runbook steps skipped.
		if setupIdx >= 0 {
			sr := allResults[setupIdx]
			setupResult = &sr
		}

		// Extract teardown result.
		if teardownIdx >= 0 {
			tr := allResults[teardownIdx]
			teardownResult = &tr
		}

		// Collect runbook step results (skip synthetic steps).
		startIdx := 0
		if setupIdx >= 0 {
			startIdx = 1
		}
		endIdx := len(allResults)
		if teardownIdx >= 0 {
			endIdx = len(allResults) - 1
		}
		results = allResults[startIdx:endIdx]

		// If setup failed, mark all runbook steps as skipped.
		if setupResult != nil && setupResult.Status == core.StatusFailed {
			reason := setupResult.Error
			if reason == "" && setupResult.ExitCode != 0 {
				reason = fmt.Sprintf("exit code %d", setupResult.ExitCode)
			}
			for i := range results {
				if results[i].Status != core.StatusSkipped {
					results[i].Status = core.StatusSkipped
					results[i].Error = "setup failed: " + reason
				}
			}
		}

		// Include setup/teardown in report metadata (not in steps).
		_ = teardownResult // teardown failure is informational only
	}

	// Build hooks metadata for the report.
	hooks := make(map[string]string)
	if setupResult != nil {
		hooks["setup"] = setupResult.Status
	}
	if teardownResult != nil {
		hooks["teardown"] = teardownResult.Status
	}

	rpt := core.Report{
		Version:     "1",
		Runbook:     name,
		Environment: effectiveEnvironment(opts.Env),
		DurationMs:  time.Since(start).Milliseconds(),
		Summary:     computeSummary(results),
		Steps:       results,
	}
	if len(hooks) > 0 {
		rpt.Hooks = hooks
	}

	return RunResult{Report: rpt, ArtifactDir: artifactDir, Cleanup: cleanup}, nil
}

func effectiveEnvironment(seed map[string]string) map[string]string {
	env := make(map[string]string, 3)
	if pwd, err := os.Getwd(); err == nil && pwd != "" {
		env["PWD"] = pwd
	}
	for _, key := range []string{"HOME", "TMPDIR"} {
		if seed != nil && seed[key] != "" {
			env[key] = seed[key]
			continue
		}
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

func reportHasRetainableFailure(r core.Report) bool {
	if r.Summary.Failed > 0 {
		return true
	}
	return r.Hooks["setup"] == core.StatusFailed
}

// computeSummary tallies step results into a Summary.
func computeSummary(results []core.StepResult) core.Summary {
	var s core.Summary
	s.Total = len(results)
	for _, r := range results {
		switch r.Status {
		case core.StatusPassed:
			s.Passed++
		case core.StatusFailed:
			s.Failed++
		case core.StatusSkipped:
			s.Skipped++
		}
	}
	return s
}
