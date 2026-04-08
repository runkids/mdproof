package report

import (
	"fmt"
	"io"

	"github.com/runkids/mdproof/internal/assertion"
	"github.com/runkids/mdproof/internal/core"
)

// WriteGitHubReport writes GitHub Actions workflow commands for failed steps.
// Passed and skipped steps produce no output.
// Format: ::error file={path},line={line}::{message}
func WriteGitHubReport(w io.Writer, reports []core.Report) {
	for _, r := range reports {
		for _, s := range r.Steps {
			if s.Status != core.StatusFailed {
				continue
			}
			writeGitHubAnnotation(w, s)
		}
	}
}

func writeGitHubAnnotation(w io.Writer, s core.StepResult) {
	path := reportPath(s.Step.File)

	for _, a := range s.Assertions {
		if a.Matched {
			continue
		}
		line := 0
		if a.Source != nil {
			line = a.Source.Start.Line
		} else if heading := headingRange(s); heading != nil {
			line = heading.Start.Line
		}

		msg := fmt.Sprintf("Step %d (%s): %s", s.Step.Number, s.Step.Title, a.Pattern)
		if a.Detail != "" {
			msg += " (" + a.Detail + ")"
		}

		writeAnnotationLine(w, path, line, msg)
	}

	// If no assertions but step failed (e.g., non-zero exit code), emit one annotation.
	if len(s.Assertions) == 0 || assertion.AllPassed(s.Assertions) {
		reason := core.StepFailReason(s)
		if reason == "" {
			reason = "step failed"
		}
		msg := fmt.Sprintf("Step %d (%s): %s", s.Step.Number, s.Step.Title, reason)
		_, line := failureHeaderLocation(s)
		writeAnnotationLine(w, path, line, msg)
	}
}

func writeAnnotationLine(w io.Writer, path string, line int, msg string) {
	if path != "" && line > 0 {
		fmt.Fprintf(w, "::error file=%s,line=%d::%s\n", path, line, msg)
	} else if path != "" {
		fmt.Fprintf(w, "::error file=%s::%s\n", path, msg)
	} else {
		fmt.Fprintf(w, "::error ::%s\n", msg)
	}
}
