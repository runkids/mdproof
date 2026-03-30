package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/runkids/mdproof/internal/core"
)

func TestWriteSingleReport_FailedAssertionShowsSourceLocation(t *testing.T) {
	report := core.Report{
		Runbook: "example.md",
		Summary: core.Summary{Total: 1, Failed: 1},
		Steps: []core.StepResult{
			{
				Step: core.Step{
					Number: 1,
					Title:  "Check hello",
					File:   "docs/example.md",
					HeadingSource: core.SourceRange{
						Start: core.SourcePos{Line: 5},
						End:   core.SourcePos{Line: 5},
					},
					CodeSources: []core.SourceRange{{
						Start: core.SourcePos{Line: 7},
						End:   core.SourcePos{Line: 9},
					}},
				},
				Source: &core.StepSource{
					Heading: &core.SourceRange{
						Start: core.SourcePos{Line: 5},
						End:   core.SourcePos{Line: 5},
					},
					CodeBlocks: []core.SourceRange{{
						Start: core.SourcePos{Line: 7},
						End:   core.SourcePos{Line: 9},
					}},
				},
				Status: core.StatusFailed,
				Assertions: []core.AssertionResult{{
					Pattern: "hello",
					Type:    core.AssertSubstring,
					Matched: false,
					Source: &core.SourceRange{
						Start: core.SourcePos{Line: 12},
						End:   core.SourcePos{Line: 12},
					},
				}},
			},
		},
	}

	var buf bytes.Buffer
	WriteSingleReport(&buf, report, 0)

	out := buf.String()
	if !strings.Contains(out, "FAIL docs/example.md:12 Step 1: Check hello") {
		t.Fatalf("plain report missing failure header with source:\n%s", out)
	}
	if !strings.Contains(out, "Assertion docs/example.md:12 hello") {
		t.Fatalf("plain report missing assertion source line:\n%s", out)
	}
}

func TestWritePlainSummary_VerboseShowsPerRunbookDetail(t *testing.T) {
	reports := []core.Report{
		{
			Runbook: "a-proof.md",
			Summary: core.Summary{Total: 1, Passed: 1},
			Steps: []core.StepResult{{
				Step:   core.Step{Number: 1, Title: "Step A"},
				Status: core.StatusPassed,
				Assertions: []core.AssertionResult{{
					Pattern: "ok", Type: core.AssertSubstring, Matched: true,
				}},
			}},
		},
		{
			Runbook: "b-proof.md",
			Summary: core.Summary{Total: 1, Passed: 1},
			Steps: []core.StepResult{{
				Step:   core.Step{Number: 1, Title: "Step B"},
				Status: core.StatusPassed,
				Assertions: []core.AssertionResult{{
					Pattern: "done", Type: core.AssertSubstring, Matched: true,
				}},
			}},
		},
	}

	// v=0: summary only, no assertion details.
	var buf0 bytes.Buffer
	WritePlainSummary(&buf0, reports, 0)
	out0 := buf0.String()
	if strings.Contains(out0, "\u2713 ok") {
		t.Fatalf("v=0 should NOT show assertion details:\n%s", out0)
	}

	// v=1: per-runbook detail + assertions shown, then summary.
	var buf1 bytes.Buffer
	WritePlainSummary(&buf1, reports, 1)
	out1 := buf1.String()
	if !strings.Contains(out1, "\u2713 ok") {
		t.Fatalf("v=1 should show assertion detail 'ok':\n%s", out1)
	}
	if !strings.Contains(out1, "\u2713 done") {
		t.Fatalf("v=1 should show assertion detail 'done':\n%s", out1)
	}
	if !strings.Contains(out1, "Runbook Results (2 files)") {
		t.Fatalf("v=1 should still show summary table:\n%s", out1)
	}
}

func TestWritePlainSummary_VeryVerboseShowsStdout(t *testing.T) {
	reports := []core.Report{{
		Runbook: "d-proof.md",
		Summary: core.Summary{Total: 1, Passed: 1},
		Steps: []core.StepResult{{
			Step:   core.Step{Number: 1, Title: "Echo test"},
			Status: core.StatusPassed,
			Stdout: "hello world\n",
			Assertions: []core.AssertionResult{{
				Pattern: "hello", Type: core.AssertSubstring, Matched: true,
			}},
		}},
	}}

	// v=1: assertions shown, but NOT stdout.
	var buf1 bytes.Buffer
	WritePlainSummary(&buf1, reports, 1)
	out1 := buf1.String()
	if !strings.Contains(out1, "\u2713 hello") {
		t.Fatalf("v=1 should show assertion detail:\n%s", out1)
	}
	if strings.Contains(out1, "stdout (1 lines)") {
		t.Fatalf("v=1 should NOT show stdout snippet:\n%s", out1)
	}

	// v=2: assertions AND stdout shown.
	var buf2 bytes.Buffer
	WritePlainSummary(&buf2, reports, 2)
	out2 := buf2.String()
	if !strings.Contains(out2, "\u2713 hello") {
		t.Fatalf("v=2 should show assertion detail:\n%s", out2)
	}
	if !strings.Contains(out2, "stdout (1 lines)") {
		t.Fatalf("v=2 should show stdout snippet:\n%s", out2)
	}
	if !strings.Contains(out2, "hello world") {
		t.Fatalf("v=2 should show actual stdout content:\n%s", out2)
	}
}

func TestWritePlainSummary_NoVerboseStillShowsFailReasons(t *testing.T) {
	reports := []core.Report{{
		Runbook: "c-proof.md",
		Summary: core.Summary{Total: 1, Failed: 1},
		Steps: []core.StepResult{{
			Step:     core.Step{Number: 1, Title: "Fail step"},
			Status:   core.StatusFailed,
			ExitCode: 1,
		}},
	}}

	var buf bytes.Buffer
	WritePlainSummary(&buf, reports, 0)
	out := buf.String()
	if !strings.Contains(out, "Step 1:") {
		t.Fatalf("v=0 summary should show inline failure reason:\n%s", out)
	}
}

func TestWriteSingleReport_CommandFailureShowsCodeBlockRange(t *testing.T) {
	report := core.Report{
		Runbook: "example.md",
		Summary: core.Summary{Total: 1, Failed: 1},
		Steps: []core.StepResult{
			{
				Step: core.Step{
					Number: 2,
					Title:  "Run command",
					File:   "docs/example.md",
					CodeSources: []core.SourceRange{{
						Start: core.SourcePos{Line: 20},
						End:   core.SourcePos{Line: 24},
					}},
				},
				Source: &core.StepSource{
					CodeBlocks: []core.SourceRange{{
						Start: core.SourcePos{Line: 20},
						End:   core.SourcePos{Line: 24},
					}},
				},
				Status:   core.StatusFailed,
				ExitCode: 2,
			},
		},
	}

	var buf bytes.Buffer
	WriteSingleReport(&buf, report, 0)

	out := buf.String()
	if !strings.Contains(out, "FAIL docs/example.md:20 Step 2: Run command") {
		t.Fatalf("plain report missing command failure header:\n%s", out)
	}
	if !strings.Contains(out, "Command docs/example.md:20-24") {
		t.Fatalf("plain report missing command source range:\n%s", out)
	}
}

func TestWriteSingleReport_FailedStepShowsDebugBlockWhenArtifactsRetained(t *testing.T) {
	report := core.Report{
		Runbook:      "debug-proof.md",
		ArtifactDir:  "/tmp/mdproof-session-123",
		IsolationDir: "/tmp/mdproof-iso-456",
		Summary:      core.Summary{Total: 1, Failed: 1},
		Steps: []core.StepResult{{
			Step:     core.Step{Number: 2, Title: "Broken step"},
			Status:   core.StatusFailed,
			ExitCode: 1,
			Debug: &core.StepDebug{
				ScriptPath: "/tmp/mdproof-session-123/step_2.sh",
				EnvPath:    "/tmp/mdproof-session-123/step_2_env",
				StdoutPath: "/tmp/mdproof-session-123/step_2_out",
				StderrPath: "/tmp/mdproof-session-123/step_2_err",
				Environment: &core.DebugEnvironment{
					PWD:    "/workspace",
					HOME:   "/tmp/mdproof-iso-456",
					TMPDIR: "/tmp/mdproof-iso-456/tmp",
				},
			},
		}},
	}

	var buf bytes.Buffer
	WriteSingleReport(&buf, report, 0)

	out := buf.String()
	for _, want := range []string{
		"artifact dir: /tmp/mdproof-session-123",
		"isolation dir: /tmp/mdproof-iso-456",
		"failed step: Step 2 Broken step",
		"script path: /tmp/mdproof-session-123/step_2.sh",
		"env path: /tmp/mdproof-session-123/step_2_env",
		"stdout path: /tmp/mdproof-session-123/step_2_out",
		"stderr path: /tmp/mdproof-session-123/step_2_err",
		"PWD=/workspace",
		"HOME=/tmp/mdproof-iso-456",
		"TMPDIR=/tmp/mdproof-iso-456/tmp",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("plain report missing %q:\n%s", want, out)
		}
	}
}

func TestWriteSingleReport_FailedStepWithoutRetainedArtifactsShowsRerunHint(t *testing.T) {
	report := core.Report{
		Runbook: "debug-proof.md",
		Summary: core.Summary{Total: 1, Failed: 1},
		Steps: []core.StepResult{{
			Step:     core.Step{Number: 1, Title: "Broken step"},
			Status:   core.StatusFailed,
			ExitCode: 1,
			Debug: &core.StepDebug{
				ScriptPath: "/tmp/mdproof-session-123/step_1.sh",
				EnvPath:    "/tmp/mdproof-session-123/step_1_env",
				StdoutPath: "/tmp/mdproof-session-123/step_1_out",
				StderrPath: "/tmp/mdproof-session-123/step_1_err",
			},
		}},
	}

	var buf bytes.Buffer
	WriteSingleReport(&buf, report, 0)

	out := buf.String()
	if !strings.Contains(out, "rerun with --keep-failed-artifacts") {
		t.Fatalf("plain report missing rerun hint:\n%s", out)
	}
	if strings.Contains(out, "script path: /tmp/mdproof-session-123/step_1.sh") {
		t.Fatalf("plain report should not advertise ephemeral artifact paths:\n%s", out)
	}
}
