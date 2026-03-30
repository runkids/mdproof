package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/runkids/mdproof/internal/core"
)

func newTestReport() core.Report {
	return core.Report{
		Version:    "1.0",
		Runbook:    "test-runbook.md",
		DurationMs: 150,
		Summary: core.Summary{
			Total:   3,
			Passed:  2,
			Failed:  0,
			Skipped: 1,
		},
		Steps: []core.StepResult{
			{
				Step:       core.Step{Number: 1, Title: "build", Command: "make build"},
				Status:     "passed",
				DurationMs: 80,
			},
			{
				Step:       core.Step{Number: 2, Title: "test", Command: "make test"},
				Status:     "passed",
				DurationMs: 60,
			},
			{
				Step:       core.Step{Number: 3, Title: "deploy", Executor: "manual"},
				Status:     "skipped",
				DurationMs: 0,
			},
		},
	}
}

func TestJSON_RoundTrip(t *testing.T) {
	report := newTestReport()
	var buf bytes.Buffer
	if err := WriteJSONReport(&buf, report); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	var got core.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Runbook != report.Runbook {
		t.Errorf("Runbook = %q, want %q", got.Runbook, report.Runbook)
	}
	if got.DurationMs != report.DurationMs {
		t.Errorf("DurationMs = %d, want %d", got.DurationMs, report.DurationMs)
	}
}

func TestJSON_SummaryFields(t *testing.T) {
	report := newTestReport()
	var buf bytes.Buffer
	if err := WriteJSONReport(&buf, report); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	var got core.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Summary.Total != 3 {
		t.Errorf("Summary.Total = %d, want 3", got.Summary.Total)
	}
	if got.Summary.Passed != 2 {
		t.Errorf("Summary.Passed = %d, want 2", got.Summary.Passed)
	}
	if got.Summary.Failed != 0 {
		t.Errorf("Summary.Failed = %d, want 0", got.Summary.Failed)
	}
	if got.Summary.Skipped != 1 {
		t.Errorf("Summary.Skipped = %d, want 1", got.Summary.Skipped)
	}
}

func TestJSON_VersionPresent(t *testing.T) {
	report := newTestReport()
	var buf bytes.Buffer
	if err := WriteJSONReport(&buf, report); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	v, ok := raw["version"]
	if !ok {
		t.Fatal("version field missing from JSON output")
	}

	var version string
	if err := json.Unmarshal(v, &version); err != nil {
		t.Fatalf("version unmarshal: %v", err)
	}
	if version != "1.0" {
		t.Errorf("version = %q, want %q", version, "1.0")
	}
}

func TestJSON_StepsLength(t *testing.T) {
	report := newTestReport()
	var buf bytes.Buffer
	if err := WriteJSONReport(&buf, report); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	var got core.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(got.Steps) != 3 {
		t.Errorf("len(Steps) = %d, want 3", len(got.Steps))
	}
}

func TestJSON_IncludesSourceMetadata(t *testing.T) {
	report := newTestReport()
	report.Steps[0].Step.File = "docs/test-runbook.md"
	report.Steps[0].Step.HeadingSource = core.SourceRange{
		Start: core.SourcePos{Line: 4},
		End:   core.SourcePos{Line: 4},
	}
	report.Steps[0].Step.CodeSources = []core.SourceRange{{
		Start: core.SourcePos{Line: 6},
		End:   core.SourcePos{Line: 8},
	}}
	report.Steps[0].Source = core.StepSourceFromStep(report.Steps[0].Step)
	report.Steps[0].Assertions = []core.AssertionResult{{
		Pattern: "hello",
		Type:    core.AssertSubstring,
		Matched: false,
		Source: &core.SourceRange{
			Start: core.SourcePos{Line: 11},
			End:   core.SourcePos{Line: 11},
		},
	}}

	var buf bytes.Buffer
	if err := WriteJSONReport(&buf, report); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	var raw struct {
		Steps []struct {
			Source struct {
				Heading struct {
					Start struct {
						Line int `json:"line"`
					} `json:"start"`
				} `json:"heading"`
				CodeBlocks []struct {
					Start struct {
						Line int `json:"line"`
					} `json:"start"`
					End struct {
						Line int `json:"line"`
					} `json:"end"`
				} `json:"code_blocks"`
			} `json:"source"`
			Assertions []struct {
				Source struct {
					Start struct {
						Line int `json:"line"`
					} `json:"start"`
				} `json:"source"`
			} `json:"assertions"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if raw.Steps[0].Source.Heading.Start.Line != 4 {
		t.Fatalf("heading start line = %d, want 4", raw.Steps[0].Source.Heading.Start.Line)
	}
	if len(raw.Steps[0].Source.CodeBlocks) != 1 || raw.Steps[0].Source.CodeBlocks[0].Start.Line != 6 || raw.Steps[0].Source.CodeBlocks[0].End.Line != 8 {
		t.Fatalf("code_blocks = %+v, want 6-8", raw.Steps[0].Source.CodeBlocks)
	}
	if len(raw.Steps[0].Assertions) != 1 || raw.Steps[0].Assertions[0].Source.Start.Line != 11 {
		t.Fatalf("assertion source = %+v, want line 11", raw.Steps[0].Assertions)
	}
}

func TestJSON_IncludesFailureObservabilityMetadata(t *testing.T) {
	report := newTestReport()
	report.Environment = map[string]string{
		"PWD":    "/workspace",
		"HOME":   "/tmp/mdproof-iso-123",
		"TMPDIR": "/tmp/mdproof-iso-123/tmp",
	}
	report.ArtifactDir = "/tmp/mdproof-session-123"
	report.IsolationDir = "/tmp/mdproof-iso-123"
	report.Steps[0].Debug = &core.StepDebug{
		ScriptPath: "/tmp/mdproof-session-123/step_1.sh",
		EnvPath:    "/tmp/mdproof-session-123/step_1_env",
		StdoutPath: "/tmp/mdproof-session-123/step_1_out",
		StderrPath: "/tmp/mdproof-session-123/step_1_err",
		Environment: &core.DebugEnvironment{
			PWD:    "/workspace",
			HOME:   "/tmp/mdproof-iso-123",
			TMPDIR: "/tmp/mdproof-iso-123/tmp",
		},
	}

	var buf bytes.Buffer
	if err := WriteJSONReport(&buf, report); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	var got core.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ArtifactDir != "/tmp/mdproof-session-123" {
		t.Fatalf("ArtifactDir = %q, want %q", got.ArtifactDir, "/tmp/mdproof-session-123")
	}
	if got.IsolationDir != "/tmp/mdproof-iso-123" {
		t.Fatalf("IsolationDir = %q, want %q", got.IsolationDir, "/tmp/mdproof-iso-123")
	}
	if got.Environment["PWD"] != "/workspace" {
		t.Fatalf("report environment PWD = %q, want %q", got.Environment["PWD"], "/workspace")
	}
	if got.Steps[0].Debug == nil {
		t.Fatal("step debug missing after JSON round-trip")
	}
	if got.Steps[0].Debug.ScriptPath != "/tmp/mdproof-session-123/step_1.sh" {
		t.Fatalf("Debug.ScriptPath = %q, want %q", got.Steps[0].Debug.ScriptPath, "/tmp/mdproof-session-123/step_1.sh")
	}
	if got.Steps[0].Debug.Environment == nil {
		t.Fatal("step debug environment missing after JSON round-trip")
	}
	if got.Steps[0].Debug.Environment.TMPDIR != "/tmp/mdproof-iso-123/tmp" {
		t.Fatalf("Debug.Environment.TMPDIR = %q, want %q", got.Steps[0].Debug.Environment.TMPDIR, "/tmp/mdproof-iso-123/tmp")
	}
}
