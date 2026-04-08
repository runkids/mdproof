package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/runkids/mdproof/internal/core"
)

func TestGitHub_PassedStepsNoOutput(t *testing.T) {
	var buf bytes.Buffer
	WriteGitHubReport(&buf, []core.Report{newTestReport()})
	if buf.Len() != 0 {
		t.Errorf("expected no output for passing report, got %q", buf.String())
	}
}

func TestGitHub_FailedAssertionWithSource(t *testing.T) {
	r := newTestReport()
	r.Steps[0].Status = core.StatusFailed
	r.Steps[0].Step.File = "runbooks/api-proof.md"
	r.Steps[0].Step.HeadingSource = core.SourceRange{
		Start: core.SourcePos{Line: 7},
		End:   core.SourcePos{Line: 7},
	}
	r.Steps[0].Assertions = []core.AssertionResult{
		{
			Pattern: "expected output",
			Type:    core.AssertSubstring,
			Matched: false,
			Detail:  "not found in stdout",
			Source: &core.SourceRange{
				Start: core.SourcePos{Line: 12},
				End:   core.SourcePos{Line: 12},
			},
		},
	}
	r.Summary.Passed = 1
	r.Summary.Failed = 1

	var buf bytes.Buffer
	WriteGitHubReport(&buf, []core.Report{r})

	got := buf.String()
	want := "::error file=runbooks/api-proof.md,line=12::Step 1 (build): expected output (not found in stdout)\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGitHub_FailedByExitCodeOnly(t *testing.T) {
	r := newTestReport()
	r.Steps[0].Status = core.StatusFailed
	r.Steps[0].ExitCode = 2
	r.Steps[0].Step.File = "runbooks/deploy-proof.md"
	r.Steps[0].Step.HeadingSource = core.SourceRange{
		Start: core.SourcePos{Line: 5},
		End:   core.SourcePos{Line: 5},
	}
	r.Steps[0].Assertions = nil
	r.Summary.Passed = 1
	r.Summary.Failed = 1

	var buf bytes.Buffer
	WriteGitHubReport(&buf, []core.Report{r})

	got := buf.String()
	want := "::error file=runbooks/deploy-proof.md,line=5::Step 1 (build): exit code 2\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGitHub_NoSourcePosition(t *testing.T) {
	r := newTestReport()
	r.Steps[0].Status = core.StatusFailed
	r.Steps[0].Assertions = []core.AssertionResult{
		{Pattern: "hello", Type: core.AssertSubstring, Matched: false},
	}
	r.Summary.Passed = 1
	r.Summary.Failed = 1

	var buf bytes.Buffer
	WriteGitHubReport(&buf, []core.Report{r})

	got := buf.String()
	want := "::error ::Step 1 (build): hello\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGitHub_MultipleFailedAssertions(t *testing.T) {
	r := newTestReport()
	r.Steps[0].Status = core.StatusFailed
	r.Steps[0].Step.File = "test.md"
	r.Steps[0].Assertions = []core.AssertionResult{
		{
			Pattern: "first",
			Type:    core.AssertSubstring,
			Matched: false,
			Source:  &core.SourceRange{Start: core.SourcePos{Line: 10}},
		},
		{
			Pattern: ".status == \"ok\"",
			Type:    core.AssertJQ,
			Matched: false,
			Detail:  "jq failed: null",
			Source:  &core.SourceRange{Start: core.SourcePos{Line: 11}},
		},
		{
			Pattern: "passing",
			Type:    core.AssertSubstring,
			Matched: true,
		},
	}
	r.Summary.Passed = 1
	r.Summary.Failed = 1

	var buf bytes.Buffer
	WriteGitHubReport(&buf, []core.Report{r})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 annotation lines, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "line=10") {
		t.Errorf("line[0] missing line=10: %q", lines[0])
	}
	if !strings.Contains(lines[1], "line=11") {
		t.Errorf("line[1] missing line=11: %q", lines[1])
	}
	if !strings.Contains(lines[1], "jq failed: null") {
		t.Errorf("line[1] missing jq detail: %q", lines[1])
	}
}
