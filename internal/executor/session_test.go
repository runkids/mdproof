package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runkids/mdproof/internal/core"
)

func TestMain(m *testing.M) {
	// Tests run on the host — allow execution for test suite.
	os.Setenv("MDPROOF_ALLOW_EXECUTE", "1")
	os.Exit(m.Run())
}

func TestIsContainerEnv_EnvOverride(t *testing.T) {
	// Already set by TestMain, so should return true.
	if !IsContainerEnv() {
		t.Fatal("expected IsContainerEnv=true with MDPROOF_ALLOW_EXECUTE set")
	}
}

func TestIsContainerEnv_NoOverride(t *testing.T) {
	old := os.Getenv("MDPROOF_ALLOW_EXECUTE")
	os.Unsetenv("MDPROOF_ALLOW_EXECUTE")
	defer os.Setenv("MDPROOF_ALLOW_EXECUTE", old)

	// On host (no /.dockerenv), should return false.
	// Inside a real container, this test still passes (/.dockerenv exists).
	got := IsContainerEnv()
	_, hasDocker := os.Stat("/.dockerenv")
	_, hasPodman := os.Stat("/run/.containerenv")
	inContainer := hasDocker == nil || hasPodman == nil

	if got != inContainer {
		t.Fatalf("IsContainerEnv=%v, expected %v (inContainer=%v)", got, inContainer, inContainer)
	}
}

func TestExecuteSession_SimpleEcho(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "echo", Command: "echo hello", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s stderr=%q)", results[0].Status, results[0].Error, results[0].Stderr)
	}
	if got := strings.TrimSpace(results[0].Stdout); got != "hello" {
		t.Fatalf("expected stdout 'hello', got %q", got)
	}
}

func TestExecuteSession_VariablePersistence(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "set var", Command: "MY_VAR=fromstep1\necho \"set MY_VAR=$MY_VAR\"", Executor: core.ExecutorAuto},
		{Number: 2, Title: "read var", Command: "echo \"got MY_VAR=$MY_VAR\"", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusPassed {
		t.Fatalf("step 1: expected passed, got %s (err=%s stderr=%q)", results[0].Status, results[0].Error, results[0].Stderr)
	}
	if results[1].Status != core.StatusPassed {
		t.Fatalf("step 2: expected passed, got %s (err=%s stderr=%q)", results[1].Status, results[1].Error, results[1].Stderr)
	}
	if !strings.Contains(results[1].Stdout, "got MY_VAR=fromstep1") {
		t.Fatalf("step 2: expected variable from step 1, got stdout=%q", results[1].Stdout)
	}
}

func TestExecuteSession_StepFailureContinues(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "fail", Command: "echo before_fail && exit 1", Executor: core.ExecutorAuto},
		{Number: 2, Title: "still runs", Command: "echo after_fail", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusFailed {
		t.Fatalf("step 1: expected failed, got %s", results[0].Status)
	}
	if results[0].ExitCode != 1 {
		t.Fatalf("step 1: expected exit code 1, got %d", results[0].ExitCode)
	}
	if results[1].Status != core.StatusPassed {
		t.Fatalf("step 2: expected passed, got %s (err=%s stderr=%q)", results[1].Status, results[1].Error, results[1].Stderr)
	}
	if !strings.Contains(results[1].Stdout, "after_fail") {
		t.Fatalf("step 2: expected 'after_fail', got %q", results[1].Stdout)
	}
}

func TestExecuteSession_SkipsManual(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "auto", Command: "echo ok", Executor: core.ExecutorAuto},
		{Number: 2, Title: "manual", Command: "echo skip", Executor: core.ExecutorManual},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusPassed {
		t.Fatalf("step 1: expected passed, got %s", results[0].Status)
	}
	if results[1].Status != core.StatusSkipped {
		t.Fatalf("step 2: expected skipped, got %s", results[1].Status)
	}
}

func TestExecuteSession_CapturesStderr(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "stderr", Command: "echo out && echo err >&2", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s)", results[0].Status, results[0].Error)
	}
	if !strings.Contains(results[0].Stdout, "out") {
		t.Errorf("expected stdout to contain 'out', got %q", results[0].Stdout)
	}
	if !strings.Contains(results[0].Stderr, "err") {
		t.Errorf("expected stderr to contain 'err', got %q", results[0].Stderr)
	}
}

func TestExecuteSession_VariableSurvivedFailedStep(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "set and fail", Command: "SURV=yes\necho set_surv\nfalse", Executor: core.ExecutorAuto},
		{Number: 2, Title: "check surv", Command: "echo \"SURV=$SURV\"", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusFailed {
		t.Fatalf("step 1: expected failed, got %s", results[0].Status)
	}
	// EXIT trap should have saved SURV even though step failed.
	if results[1].Status != core.StatusPassed {
		t.Fatalf("step 2: expected passed, got %s (err=%s stderr=%q)", results[1].Status, results[1].Error, results[1].Stderr)
	}
	if !strings.Contains(results[1].Stdout, "SURV=yes") {
		t.Fatalf("step 2: expected SURV=yes, got %q", results[1].Stdout)
	}
}

func TestExecuteSession_Assertions(t *testing.T) {
	steps := []core.Step{
		{
			Number:   1,
			Title:    "with expected",
			Command:  "echo apple banana",
			Expected: core.Expectations("apple", "cherry"),
			Executor: core.ExecutorAuto,
		},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	// Command succeeds but assertion for "cherry" fails.
	if results[0].Status != core.StatusFailed {
		t.Fatalf("expected failed due to assertion, got %s", results[0].Status)
	}
	if len(results[0].Assertions) != 2 {
		t.Fatalf("expected 2 assertions, got %d", len(results[0].Assertions))
	}
	if !results[0].Assertions[0].Matched {
		t.Error("'apple' assertion should have matched")
	}
	if results[0].Assertions[1].Matched {
		t.Error("'cherry' assertion should NOT have matched")
	}
}

func TestExecuteSession_EmptySteps(t *testing.T) {
	results := ExecuteSession(context.Background(), nil, SessionOptions{Timeout: 30 * time.Second})
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestExecuteSession_MergedCodeBlocks(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "merged", Command: "echo first\n---\necho second", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s stderr=%q)", results[0].Status, results[0].Error, results[0].Stderr)
	}
	if !strings.Contains(results[0].Stdout, "first") || !strings.Contains(results[0].Stdout, "second") {
		t.Fatalf("expected both 'first' and 'second', got %q", results[0].Stdout)
	}
}

func TestExecuteSession_FailFast(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "fail", Command: "echo step1 && exit 1", Executor: core.ExecutorAuto},
		{Number: 2, Title: "skip", Command: "echo step2", Executor: core.ExecutorAuto},
		{Number: 3, Title: "skip", Command: "echo step3", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second, FailFast: true})

	if results[0].Status != core.StatusFailed {
		t.Errorf("step 1: expected failed, got %s", results[0].Status)
	}
	if results[1].Status != core.StatusSkipped {
		t.Errorf("step 2: expected skipped, got %s", results[1].Status)
	}
	if results[2].Status != core.StatusSkipped {
		t.Errorf("step 3: expected skipped, got %s", results[2].Status)
	}
}

func TestExecuteSession_EnvSeeding(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "check env", Command: "echo MY_VAR=$MY_VAR", Executor: core.ExecutorAuto},
	}
	env := map[string]string{"MY_VAR": "seeded"}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second, EnvVars: env})

	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s)", results[0].Status, results[0].Error)
	}
	if !strings.Contains(results[0].Stdout, "MY_VAR=seeded") {
		t.Errorf("expected MY_VAR=seeded, got %q", results[0].Stdout)
	}
}

func TestExecuteSession_Retry(t *testing.T) {
	// Uses a temp file as counter: first attempt creates file + fails,
	// second attempt sees file + passes.
	steps := []core.Step{
		{
			Number:   1,
			Title:    "retry step",
			Command:  "F=/tmp/rb_test_retry_$$\nif [ -f \"$F\" ]; then\n  echo passed\n  rm -f \"$F\"\nelse\n  touch \"$F\"\n  echo failed && exit 1\nfi",
			Retry:    1,
			Executor: core.ExecutorAuto,
		},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusPassed {
		t.Errorf("expected passed after retry, got %s (stdout=%q stderr=%q)",
			results[0].Status, results[0].Stdout, results[0].Stderr)
	}
}

func TestExecuteSession_DependsOn(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "fail", Command: "exit 1", Executor: core.ExecutorAuto},
		{Number: 2, Title: "depends", Command: "echo should_not_run", DependsOn: 1, Executor: core.ExecutorAuto},
		{Number: 3, Title: "independent", Command: "echo ok", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusFailed {
		t.Errorf("step 1: expected failed, got %s", results[0].Status)
	}
	if results[1].Status != core.StatusSkipped {
		t.Errorf("step 2: expected skipped (depends), got %s", results[1].Status)
	}
	if !strings.Contains(results[1].Error, "depends") {
		t.Errorf("step 2 error should mention depends, got: %q", results[1].Error)
	}
	if results[2].Status != core.StatusPassed {
		t.Errorf("step 3: expected passed, got %s (err=%s)", results[2].Status, results[2].Error)
	}
}

func TestExecuteSession_DependsOnPasses(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "pass", Command: "echo ok", Executor: core.ExecutorAuto},
		{Number: 2, Title: "depends", Command: "echo depends_ok", DependsOn: 1, Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusPassed {
		t.Errorf("step 1: expected passed, got %s", results[0].Status)
	}
	if results[1].Status != core.StatusPassed {
		t.Errorf("step 2: expected passed (depends satisfied), got %s (err=%s)", results[1].Status, results[1].Error)
	}
	if !strings.Contains(results[1].Stdout, "depends_ok") {
		t.Errorf("step 2: expected 'depends_ok', got %q", results[1].Stdout)
	}
}

func TestExecuteSession_SubCommandSplit(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "sub-cmds", Command: "echo first\n---\necho second", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})
	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s stderr=%q)", results[0].Status, results[0].Error, results[0].Stderr)
	}
	if len(results[0].SubCommands) != 2 {
		t.Fatalf("expected 2 sub-commands, got %d", len(results[0].SubCommands))
	}
	if !strings.Contains(results[0].SubCommands[0].Stdout, "first") {
		t.Errorf("sub 0 stdout: expected 'first', got %q", results[0].SubCommands[0].Stdout)
	}
	if !strings.Contains(results[0].SubCommands[1].Stdout, "second") {
		t.Errorf("sub 1 stdout: expected 'second', got %q", results[0].SubCommands[1].Stdout)
	}
	if !strings.Contains(results[0].Stdout, "first") || !strings.Contains(results[0].Stdout, "second") {
		t.Errorf("top-level stdout should contain both, got %q", results[0].Stdout)
	}
}

func TestExecuteSession_SubCommandFailure(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "sub-fail", Command: "echo ok\n---\nexit 1\n---\necho after", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})
	if results[0].Status != core.StatusFailed {
		t.Fatalf("expected failed, got %s", results[0].Status)
	}
	if len(results[0].SubCommands) != 3 {
		t.Fatalf("expected 3 sub-commands, got %d", len(results[0].SubCommands))
	}
	if results[0].SubCommands[0].ExitCode != 0 {
		t.Errorf("sub 0: expected exit 0, got %d", results[0].SubCommands[0].ExitCode)
	}
	if results[0].SubCommands[1].ExitCode != 1 {
		t.Errorf("sub 1: expected exit 1, got %d", results[0].SubCommands[1].ExitCode)
	}
	if results[0].SubCommands[2].ExitCode != 0 {
		t.Errorf("sub 2: expected exit 0 (still runs), got %d", results[0].SubCommands[2].ExitCode)
	}
	if results[0].ExitCode != 1 {
		t.Errorf("step exit code: expected 1, got %d", results[0].ExitCode)
	}
}

func TestExecuteSession_SubCommandVariablePersistence(t *testing.T) {
	// Block 1 exports a variable, block 2 should see it.
	steps := []core.Step{
		{Number: 1, Title: "sub-var", Command: "export SUB_VAR=from_block1\necho \"set=$SUB_VAR\"\n---\necho \"got=$SUB_VAR\"", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})
	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s stderr=%q)", results[0].Status, results[0].Error, results[0].Stderr)
	}
	if len(results[0].SubCommands) != 2 {
		t.Fatalf("expected 2 sub-commands, got %d", len(results[0].SubCommands))
	}
	if !strings.Contains(results[0].SubCommands[0].Stdout, "set=from_block1") {
		t.Errorf("sub 0: expected 'set=from_block1', got %q", results[0].SubCommands[0].Stdout)
	}
	if !strings.Contains(results[0].SubCommands[1].Stdout, "got=from_block1") {
		t.Errorf("sub 1: expected 'got=from_block1', got %q", results[0].SubCommands[1].Stdout)
	}
}

func TestExecuteSession_SingleCommandNoSubCommands(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "single", Command: "echo hello", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})
	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s", results[0].Status)
	}
	if len(results[0].SubCommands) != 0 {
		t.Errorf("expected no sub-commands for single command, got %d", len(results[0].SubCommands))
	}
}

func TestExecuteSession_StepSetup(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "step1", Command: "echo step1", Executor: core.ExecutorAuto},
		{Number: 2, Title: "step2", Command: "echo step2", Executor: core.ExecutorAuto},
	}
	opts := SessionOptions{
		Timeout:   30 * time.Second,
		StepSetup: "echo setup-ran",
	}
	results := ExecuteSession(context.Background(), steps, opts)
	for i, r := range results {
		if r.Status != core.StatusPassed {
			t.Errorf("step %d: expected passed, got %s (err=%s)", i+1, r.Status, r.Error)
		}
		if r.StepSetup == nil {
			t.Errorf("step %d: expected StepSetup result", i+1)
		} else if r.StepSetup.ExitCode != 0 {
			t.Errorf("step %d: setup exit code = %d, want 0", i+1, r.StepSetup.ExitCode)
		}
	}
	// Step stdout should NOT contain setup output
	if strings.Contains(results[0].Stdout, "setup-ran") {
		t.Errorf("step stdout should not contain setup output, got %q", results[0].Stdout)
	}
}

func TestExecuteSession_StepSetupFailure(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "step1", Command: "echo should-not-run", Executor: core.ExecutorAuto},
	}
	opts := SessionOptions{
		Timeout:   30 * time.Second,
		StepSetup: "exit 1",
	}
	results := ExecuteSession(context.Background(), steps, opts)
	if results[0].Status != core.StatusFailed {
		t.Fatalf("expected failed, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Error, "step-setup failed") {
		t.Errorf("expected setup failure error, got %q", results[0].Error)
	}
	if results[0].Stdout != "" {
		t.Errorf("step body should not run, got stdout=%q", results[0].Stdout)
	}
}

func TestExecuteSession_StepTeardownFailureIgnored(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "step1", Command: "echo hello", Executor: core.ExecutorAuto},
	}
	opts := SessionOptions{
		Timeout:      30 * time.Second,
		StepTeardown: "exit 1",
	}
	results := ExecuteSession(context.Background(), steps, opts)
	// Step should still pass despite teardown failure
	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s", results[0].Status)
	}
	if results[0].StepTeardown == nil {
		t.Fatal("expected StepTeardown result")
	}
	if results[0].StepTeardown.ExitCode != 1 {
		t.Errorf("teardown exit code: expected 1, got %d", results[0].StepTeardown.ExitCode)
	}
}

func TestExecuteSession_StepSetupWithSubCommands(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "setup+subs", Command: "echo first\n---\necho second", Executor: core.ExecutorAuto},
	}
	opts := SessionOptions{
		Timeout:   30 * time.Second,
		StepSetup: "echo cleanup",
	}
	results := ExecuteSession(context.Background(), steps, opts)
	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s", results[0].Status)
	}
	if results[0].StepSetup == nil {
		t.Fatal("expected StepSetup result")
	}
	if len(results[0].SubCommands) != 2 {
		t.Fatalf("expected 2 sub-commands, got %d", len(results[0].SubCommands))
	}
	if strings.Contains(results[0].Stdout, "cleanup") {
		t.Errorf("step stdout should not contain setup output")
	}
}

func TestExecuteSession_PerStepTimeout(t *testing.T) {
	steps := []core.Step{
		{
			Number:   1,
			Title:    "slow step",
			Command:  "sleep 10 && echo done",
			Timeout:  1 * time.Second,
			Executor: core.ExecutorAuto,
		},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	if results[0].Status != core.StatusFailed {
		t.Errorf("expected failed (timeout), got %s", results[0].Status)
	}
	if results[0].ExitCode == 0 {
		t.Errorf("expected non-zero exit code from timeout")
	}
}

func TestExecuteSession_RetryWithStepSetup(t *testing.T) {
	// Step setup creates a counter file. Step body reads and increments it.
	// With retry=2, step-setup re-runs on each attempt.
	steps := []core.Step{
		{
			Number: 1, Title: "retry-setup",
			Command: `count=$(cat /tmp/mdproof-retry-count 2>/dev/null || echo 0)
echo "attempt=$count"
[ "$count" -ge 2 ] || exit 1`,
			Executor: core.ExecutorAuto,
			Retry:    2,
		},
	}
	opts := SessionOptions{
		Timeout:      30 * time.Second,
		StepSetup:    `count=$(cat /tmp/mdproof-retry-count 2>/dev/null || echo 0); echo $((count+1)) > /tmp/mdproof-retry-count`,
		StepTeardown: "echo teardown-ran",
	}
	// Clean up before test
	os.Remove("/tmp/mdproof-retry-count")

	results := ExecuteSession(context.Background(), steps, opts)

	// Should pass after retries (setup increments counter, body checks >= 2)
	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s stdout=%q)", results[0].Status, results[0].Error, results[0].Stdout)
	}
	// Setup should have run (result populated)
	if results[0].StepSetup == nil {
		t.Error("expected StepSetup result")
	}
	// Teardown should have run
	if results[0].StepTeardown == nil {
		t.Error("expected StepTeardown result")
	}

	os.Remove("/tmp/mdproof-retry-count")
}

func TestExecuteSession_SubCommandFailFast(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "ff-sub", Command: "echo ok\n---\nexit 1\n---\necho should-skip", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{
		Timeout: 30 * time.Second, FailFast: true,
	})
	if results[0].Status != core.StatusFailed {
		t.Fatalf("expected failed, got %s", results[0].Status)
	}
	// With fail-fast, sub 2 should be skipped (no SUB_BEGIN/END emitted)
	// but we should still have at least 2 sub-commands in SubCommands
	if len(results[0].SubCommands) < 2 {
		t.Fatalf("expected at least 2 sub-commands, got %d", len(results[0].SubCommands))
	}
	if results[0].SubCommands[0].ExitCode != 0 {
		t.Errorf("sub 0: expected exit 0, got %d", results[0].SubCommands[0].ExitCode)
	}
	if results[0].SubCommands[1].ExitCode != 1 {
		t.Errorf("sub 1: expected exit 1, got %d", results[0].SubCommands[1].ExitCode)
	}
}

func TestExecuteSession_EmptySubCommandFiltered(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "empty-sub", Command: "echo first\n---\n\n---\necho last", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})
	if results[0].Status != core.StatusPassed {
		t.Fatalf("expected passed, got %s (err=%s)", results[0].Status, results[0].Error)
	}
	// Empty middle sub-command should be filtered out
	if len(results[0].SubCommands) != 2 {
		t.Fatalf("expected 2 sub-commands (empty filtered), got %d", len(results[0].SubCommands))
	}
	if !strings.Contains(results[0].SubCommands[0].Stdout, "first") {
		t.Errorf("sub 0: expected 'first', got %q", results[0].SubCommands[0].Stdout)
	}
	if !strings.Contains(results[0].SubCommands[1].Stdout, "last") {
		t.Errorf("sub 1: expected 'last', got %q", results[0].SubCommands[1].Stdout)
	}
}

func TestExecuteSession_FailedStepArtifactsRetainedWhenRequested(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "fail", Command: "echo boom && exit 1", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{
		Timeout:             30 * time.Second,
		KeepFailedArtifacts: true,
		EnvVars: map[string]string{
			"HOME":   "/tmp/mdproof-home",
			"TMPDIR": "/tmp/mdproof-home/tmp",
		},
	})

	debug := results[0].Debug
	if debug == nil {
		t.Fatal("expected debug metadata for failed step")
	}
	for _, path := range []string{debug.ScriptPath, debug.EnvPath, debug.StdoutPath, debug.StderrPath} {
		if path == "" {
			t.Fatal("expected all debug paths to be populated")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected retained artifact %q to exist: %v", path, err)
		}
	}

	scriptData, err := os.ReadFile(debug.ScriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if !strings.Contains(string(scriptData), "echo boom && exit 1") {
		t.Fatalf("script file missing command content:\n%s", string(scriptData))
	}

	envData, err := os.ReadFile(debug.EnvPath)
	if err != nil {
		t.Fatalf("read env snapshot: %v", err)
	}
	for _, want := range []string{"PWD=", "HOME=/tmp/mdproof-home", "TMPDIR=/tmp/mdproof-home/tmp"} {
		if !strings.Contains(string(envData), want) {
			t.Fatalf("env snapshot missing %q:\n%s", want, string(envData))
		}
	}

	stdoutData, err := os.ReadFile(debug.StdoutPath)
	if err != nil {
		t.Fatalf("read stdout artifact: %v", err)
	}
	if !strings.Contains(string(stdoutData), "boom") {
		t.Fatalf("stdout artifact missing step output:\n%s", string(stdoutData))
	}

	if debug.Environment == nil {
		t.Fatal("expected inline debug environment")
	}
	if debug.Environment.HOME != "/tmp/mdproof-home" {
		t.Fatalf("debug HOME = %q, want %q", debug.Environment.HOME, "/tmp/mdproof-home")
	}

	_ = os.RemoveAll(filepath.Dir(debug.ScriptPath))
}

func TestExecuteSession_FailedStepArtifactsRemovedWithoutKeep(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "fail", Command: "echo boom && exit 1", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{Timeout: 30 * time.Second})

	debug := results[0].Debug
	if debug == nil {
		t.Fatal("expected debug metadata for failed step")
	}
	if _, err := os.Stat(debug.ScriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected script artifact to be cleaned up, got err=%v", err)
	}
}

func TestExecuteSession_SubcommandFailureDebugTargetsFirstFailingSubcommand(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "subcmd fail", Command: "echo first\n---\necho second && exit 3\n---\necho third", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{
		Timeout:             30 * time.Second,
		KeepFailedArtifacts: true,
	})

	debug := results[0].Debug
	if debug == nil {
		t.Fatal("expected debug metadata for failed subcommand step")
	}
	if !strings.HasSuffix(debug.ScriptPath, "step_1_sub_1.sh") {
		t.Fatalf("expected failing subcommand script, got %q", debug.ScriptPath)
	}
	stdoutData, err := os.ReadFile(debug.StdoutPath)
	if err != nil {
		t.Fatalf("read subcommand stdout: %v", err)
	}
	if strings.TrimSpace(string(stdoutData)) != "second" {
		t.Fatalf("expected failing subcommand stdout, got %q", string(stdoutData))
	}

	_ = os.RemoveAll(filepath.Dir(debug.ScriptPath))
}

func TestExecuteSession_AssertionFailureDebugTargetsMainStepArtifacts(t *testing.T) {
	steps := []core.Step{{
		Number:   1,
		Title:    "assert fail",
		Command:  "echo apple",
		Expected: core.Expectations("banana"),
		Executor: core.ExecutorAuto,
	}}
	results := ExecuteSession(context.Background(), steps, SessionOptions{
		Timeout:             30 * time.Second,
		KeepFailedArtifacts: true,
	})

	debug := results[0].Debug
	if debug == nil {
		t.Fatal("expected debug metadata for assertion failure")
	}
	if !strings.HasSuffix(debug.ScriptPath, "step_1.sh") {
		t.Fatalf("expected main step script path, got %q", debug.ScriptPath)
	}
	stdoutData, err := os.ReadFile(debug.StdoutPath)
	if err != nil {
		t.Fatalf("read stdout artifact: %v", err)
	}
	if strings.TrimSpace(string(stdoutData)) != "apple" {
		t.Fatalf("stdout artifact = %q, want %q", string(stdoutData), "apple")
	}

	_ = os.RemoveAll(filepath.Dir(debug.ScriptPath))
}

func TestExecuteSession_StepSetupFailureDebugTargetsSetupArtifacts(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Title: "setup fail", Command: "echo should-not-run", Executor: core.ExecutorAuto},
	}
	results := ExecuteSession(context.Background(), steps, SessionOptions{
		Timeout:             30 * time.Second,
		KeepFailedArtifacts: true,
		StepSetup:           "echo setup-boom && exit 4",
	})

	debug := results[0].Debug
	if debug == nil {
		t.Fatal("expected debug metadata for setup failure")
	}
	if !strings.HasSuffix(debug.ScriptPath, "step_1_setup.sh") {
		t.Fatalf("expected setup script path, got %q", debug.ScriptPath)
	}
	stdoutData, err := os.ReadFile(debug.StdoutPath)
	if err != nil {
		t.Fatalf("read setup stdout artifact: %v", err)
	}
	if strings.TrimSpace(string(stdoutData)) != "setup-boom" {
		t.Fatalf("setup stdout artifact = %q, want %q", string(stdoutData), "setup-boom")
	}

	_ = os.RemoveAll(filepath.Dir(debug.ScriptPath))
}
