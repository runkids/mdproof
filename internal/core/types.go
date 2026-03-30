package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Status constants
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Executor mode constants
const (
	ExecutorAuto   = "auto"
	ExecutorManual = "manual"
)

// Assertion type constants.
const (
	AssertSubstring = "substring"
	AssertExitCode  = "exit_code"
	AssertRegex     = "regex"
	AssertJQ        = "jq"
	AssertSnapshot  = "snapshot"
)

// Default timeouts.
const (
	DefaultStepTimeout    = 2 * time.Minute
	DefaultSessionTimeout = 10 * time.Minute
)

// Sentinel exit codes used by the session executor.
const (
	ExitCodeFailFastSkipped = -1 // step skipped due to --fail-fast
	ExitCodeDependsSkipped  = -2 // step skipped due to depends directive
)

// SourcePos identifies a 1-based line location in a Markdown file.
type SourcePos struct {
	Line int `json:"line"`
}

// SourceRange identifies a range of lines in a Markdown file.
type SourceRange struct {
	Start SourcePos `json:"start"`
	End   SourcePos `json:"end"`
}

// IsZero reports whether the range has no source information.
func (r SourceRange) IsZero() bool {
	return r.Start.Line == 0 && r.End.Line == 0
}

// Expectation is a parsed assertion with source metadata.
type Expectation struct {
	Text   string      `json:"-"`
	Source SourceRange `json:"-"`
}

// StepSource is the report-friendly source view for a step.
type StepSource struct {
	Heading    *SourceRange  `json:"heading,omitempty"`
	CodeBlocks []SourceRange `json:"code_blocks,omitempty"`
}

// Step represents a single test step in a runbook.
type Step struct {
	Number        int           `json:"number"`
	Title         string        `json:"title"`
	Description   string        `json:"description,omitempty"`
	Command       string        `json:"command,omitempty"`
	Lang          string        `json:"lang,omitempty"`
	Expected      []Expectation `json:"-"`
	Executor      string        `json:"executor,omitempty"` // "auto", "ai-delegate", "manual"
	Timeout       time.Duration `json:"timeout,omitempty"`  // per-step timeout override (0 = use global)
	Retry         int           `json:"retry,omitempty"`    // retry count on failure (0 = no retry)
	RetryDelay    time.Duration `json:"retry_delay,omitempty"`
	DependsOn     int           `json:"depends_on,omitempty"` // skip if this step number failed (0 = none)
	File          string        `json:"-"`
	HeadingSource SourceRange   `json:"-"`
	CodeSources   []SourceRange `json:"-"`
}

// HookExecResult holds the outcome of a step-setup or step-teardown execution.
type HookExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// DebugEnvironment captures the key execution context for a step or hook.
type DebugEnvironment struct {
	PWD    string `json:"pwd,omitempty"`
	HOME   string `json:"home,omitempty"`
	TMPDIR string `json:"tmpdir,omitempty"`
}

// StepDebug describes the shell artifacts for the execution unit that matters
// most for debugging a step failure.
type StepDebug struct {
	ScriptPath  string            `json:"script_path,omitempty"`
	EnvPath     string            `json:"env_path,omitempty"`
	StdoutPath  string            `json:"stdout_path,omitempty"`
	StderrPath  string            `json:"stderr_path,omitempty"`
	Environment *DebugEnvironment `json:"environment,omitempty"`
}

// SubCommandResult holds the outcome of a single sub-command within a step.
type SubCommandResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// StepResult represents the execution result of a single step.
type StepResult struct {
	Step         Step               `json:"step"`
	Source       *StepSource        `json:"source,omitempty"`
	Status       string             `json:"status"` // "passed", "failed", "skipped", "running"
	DurationMs   int64              `json:"duration_ms"`
	Stdout       string             `json:"stdout,omitempty"`
	Stderr       string             `json:"stderr,omitempty"`
	ExitCode     int                `json:"exit_code"`
	Assertions   []AssertionResult  `json:"assertions,omitempty"`
	Error        string             `json:"error,omitempty"`
	StepSetup    *HookExecResult    `json:"step_setup,omitempty"`
	StepTeardown *HookExecResult    `json:"step_teardown,omitempty"`
	SubCommands  []SubCommandResult `json:"sub_commands,omitempty"`
	Debug        *StepDebug         `json:"debug,omitempty"`
}

// AssertionResult represents the result of a single assertion check.
type AssertionResult struct {
	Pattern string       `json:"pattern"`
	Type    string       `json:"type,omitempty"` // "substring", "exit_code", "regex", "jq"
	Matched bool         `json:"matched"`
	Negated bool         `json:"negated,omitempty"`
	Detail  string       `json:"detail,omitempty"` // extra info on failure (e.g., "got exit_code=1")
	Source  *SourceRange `json:"source,omitempty"`
}

// Report represents the full execution report for a runbook.
type Report struct {
	Version      string            `json:"version"`
	Runbook      string            `json:"runbook"`
	Environment  map[string]string `json:"environment,omitempty"`
	ArtifactDir  string            `json:"artifact_dir,omitempty"`
	IsolationDir string            `json:"isolation_dir,omitempty"`
	Hooks        map[string]string `json:"hooks,omitempty"` // setup/teardown status
	DurationMs   int64             `json:"duration_ms"`
	Summary      Summary           `json:"summary"`
	Steps        []StepResult      `json:"steps"`
}

// Summary represents execution summary counts.
type Summary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// Meta represents metadata extracted from runbook headings.
type Meta struct {
	Title string
	Scope string
	Env   string
}

// Runbook is the fully parsed representation of an E2E runbook Markdown file.
type Runbook struct {
	Meta  Meta
	Steps []Step
}

type stepJSON struct {
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Command     string        `json:"command,omitempty"`
	Lang        string        `json:"lang,omitempty"`
	Expected    []string      `json:"expected,omitempty"`
	Executor    string        `json:"executor,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	Retry       int           `json:"retry,omitempty"`
	RetryDelay  time.Duration `json:"retry_delay,omitempty"`
	DependsOn   int           `json:"depends_on,omitempty"`
}

// MarshalJSON preserves the historical JSON shape where step.expected is []string.
func (s Step) MarshalJSON() ([]byte, error) {
	return json.Marshal(stepJSON{
		Number:      s.Number,
		Title:       s.Title,
		Description: s.Description,
		Command:     s.Command,
		Lang:        s.Lang,
		Expected:    ExpectationTexts(s.Expected),
		Executor:    s.Executor,
		Timeout:     s.Timeout,
		Retry:       s.Retry,
		RetryDelay:  s.RetryDelay,
		DependsOn:   s.DependsOn,
	})
}

// UnmarshalJSON accepts the historical JSON shape where step.expected is []string.
func (s *Step) UnmarshalJSON(data []byte) error {
	var aux stepJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	s.Number = aux.Number
	s.Title = aux.Title
	s.Description = aux.Description
	s.Command = aux.Command
	s.Lang = aux.Lang
	s.Expected = Expectations(aux.Expected...)
	s.Executor = aux.Executor
	s.Timeout = aux.Timeout
	s.Retry = aux.Retry
	s.RetryDelay = aux.RetryDelay
	s.DependsOn = aux.DependsOn
	return nil
}

// NewExpectation creates an expectation without source metadata.
func NewExpectation(text string) Expectation {
	return Expectation{Text: text}
}

// Expectations converts plain text assertions into expectations.
func Expectations(texts ...string) []Expectation {
	expected := make([]Expectation, 0, len(texts))
	for _, text := range texts {
		expected = append(expected, NewExpectation(text))
	}
	return expected
}

// ExpectationTexts extracts assertion text for matching and JSON compatibility.
func ExpectationTexts(expected []Expectation) []string {
	if len(expected) == 0 {
		return nil
	}
	texts := make([]string, 0, len(expected))
	for _, exp := range expected {
		texts = append(texts, exp.Text)
	}
	return texts
}

// StepSourceFromStep builds the report-friendly source view for a step.
func StepSourceFromStep(step Step) *StepSource {
	var source StepSource
	if !step.HeadingSource.IsZero() {
		heading := step.HeadingSource
		source.Heading = &heading
	}
	if len(step.CodeSources) > 0 {
		source.CodeBlocks = append(source.CodeBlocks, step.CodeSources...)
	}
	if source.Heading == nil && len(source.CodeBlocks) == 0 {
		return nil
	}
	return &source
}

// SortedKeys returns map keys in sorted order for deterministic output.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TruncateText shortens s to max characters, adding ellipsis if needed.
func TruncateText(s string, max int) string {
	if len(s) <= max {
		return s // fast path: byte len fits means rune len also fits
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return "\u2026"
	}
	return string(runes[:max-1]) + "\u2026"
}

// FormatDurationMs formats milliseconds into a human-readable string.
func FormatDurationMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// StepFailReason extracts a concise failure reason from a StepResult.
func StepFailReason(r StepResult) string {
	for _, a := range r.Assertions {
		if !a.Matched {
			if a.Negated {
				return fmt.Sprintf("unexpected match: %s", a.Pattern)
			}
			return fmt.Sprintf("expected: %s", a.Pattern)
		}
	}
	if r.Error != "" {
		return r.Error
	}
	if r.ExitCode != 0 {
		return fmt.Sprintf("exit code %d", r.ExitCode)
	}
	return ""
}
