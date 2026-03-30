package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/runkids/mdproof/internal/core"
)

// WriteSingleReport prints a single runbook result in plain text.
// verbosity: 0=default, 1=show assertions, 2=assertions+stdout/stderr.
func WriteSingleReport(w io.Writer, r core.Report, verbosity int) {
	icon := "\u2713"
	if r.Summary.Failed > 0 {
		icon = "\u2717"
	}

	fmt.Fprintf(w, "\n %s %s\n", icon, r.Runbook)
	fmt.Fprintf(w, " %s\n", strings.Repeat("\u2500", 50))

	// Show hook status if present.
	if status, ok := r.Hooks["setup"]; ok {
		hIcon := plainStatusIcon(status)
		fmt.Fprintf(w, " %s  [setup]\n", hIcon)
		if status == core.StatusFailed {
			fmt.Fprintf(w, "          \u2514\u2500 setup failed, all steps skipped\n")
		}
	}

	for _, s := range r.Steps {
		sIcon := plainStatusIcon(s.Status)
		fmt.Fprintf(w, " %s  Step %-2d %-38s", sIcon, s.Step.Number, core.TruncateText(s.Step.Title, 38))
		if s.Status == core.StatusPassed || s.Status == core.StatusFailed {
			fmt.Fprintf(w, " %s", core.FormatDurationMs(s.DurationMs))
		}
		fmt.Fprintln(w)

		if verbosity == 0 {
			if s.Status == core.StatusFailed {
				if path, line := failureHeaderLocation(s); path != "" && line > 0 {
					fmt.Fprintf(w, "          FAIL %s:%d Step %d: %s\n", path, line, s.Step.Number, s.Step.Title)
				}
				if loc, pattern := assertionFailureLocation(s); loc != "" {
					fmt.Fprintf(w, "          Assertion %s %s\n", loc, pattern)
				} else if loc := commandFailureLocation(s); loc != "" {
					fmt.Fprintf(w, "          Command %s\n", loc)
				}
				reason := core.StepFailReason(s)
				if reason != "" {
					fmt.Fprintf(w, "          \u2514\u2500 %s\n", reason)
				}
				// Show first failing sub-command at v=0.
				if len(s.SubCommands) > 0 {
					for i, sc := range s.SubCommands {
						if sc.ExitCode != 0 {
							cmd := core.TruncateText(strings.ReplaceAll(sc.Command, "\n", "; "), 50)
							fmt.Fprintf(w, "          \u2514\u2500 sub[%d] failed: %s\n", i, cmd)
							break
						}
					}
				}
			} else if s.Status == core.StatusSkipped && s.Error != "" && s.Error != "manual step" {
				fmt.Fprintf(w, "          \u2514\u2500 %s\n", s.Error)
			}
		}

		// -v: show all assertions
		if verbosity >= 1 {
			writeAssertionDetails(w, s)
		}

		// -vv: show stdout/stderr snippets
		if verbosity >= 2 {
			writeOutputSnippet(w, "stdout", s.Stdout)
			writeOutputSnippet(w, "stderr", s.Stderr)
			// Show sub-command details.
			if len(s.SubCommands) > 0 {
				for i, sc := range s.SubCommands {
					scIcon := plainStatusIcon(core.StatusPassed)
					if sc.ExitCode != 0 {
						scIcon = plainStatusIcon(core.StatusFailed)
					}
					fmt.Fprintf(w, "          %s sub[%d] exit=%d\n", scIcon, i, sc.ExitCode)
					if sc.ExitCode != 0 {
						cmd := core.TruncateText(strings.ReplaceAll(sc.Command, "\n", "; "), 60)
						fmt.Fprintf(w, "            cmd: %s\n", cmd)
						if sc.Stderr != "" {
							writeOutputSnippet(w, "stderr", sc.Stderr)
						}
					}
				}
			}
		}
	}

	if failed := firstFailedStepWithDebug(r); failed != nil {
		if r.ArtifactDir != "" {
			fmt.Fprintf(w, "          artifact dir: %s\n", r.ArtifactDir)
			if r.IsolationDir != "" {
				fmt.Fprintf(w, "          isolation dir: %s\n", r.IsolationDir)
			}
			fmt.Fprintf(w, "          failed step: Step %d %s\n", failed.Step.Number, failed.Step.Title)
			fmt.Fprintf(w, "          script path: %s\n", failed.Debug.ScriptPath)
			fmt.Fprintf(w, "          env path: %s\n", failed.Debug.EnvPath)
			fmt.Fprintf(w, "          stdout path: %s\n", failed.Debug.StdoutPath)
			fmt.Fprintf(w, "          stderr path: %s\n", failed.Debug.StderrPath)
			if failed.Debug.Environment != nil {
				fmt.Fprintf(w, "          PWD=%s\n", failed.Debug.Environment.PWD)
				fmt.Fprintf(w, "          HOME=%s\n", failed.Debug.Environment.HOME)
				fmt.Fprintf(w, "          TMPDIR=%s\n", failed.Debug.Environment.TMPDIR)
			}
		} else {
			fmt.Fprintf(w, "          rerun with --keep-failed-artifacts to preserve file paths\n")
		}
	}

	// Show teardown status if present.
	if status, ok := r.Hooks["teardown"]; ok {
		hIcon := plainStatusIcon(status)
		fmt.Fprintf(w, " %s  [teardown]\n", hIcon)
	}

	fmt.Fprintf(w, " %s\n", strings.Repeat("\u2500", 50))
	fmt.Fprintf(w, " %d/%d passed", r.Summary.Passed, r.Summary.Total)
	if r.Summary.Failed > 0 {
		fmt.Fprintf(w, "  %d failed", r.Summary.Failed)
	}
	if r.Summary.Skipped > 0 {
		fmt.Fprintf(w, "  %d skipped", r.Summary.Skipped)
	}
	fmt.Fprintf(w, "  %s\n\n", core.FormatDurationMs(r.DurationMs))
}

// WritePlainSummary prints a multi-runbook batch summary.
// When verbosity > 0, each report is printed in detail first (like
// `go test -v ./...`), followed by the summary table.
func WritePlainSummary(w io.Writer, reports []core.Report, verbosity int) {
	// -v / -vv: print per-runbook details before the summary.
	if verbosity > 0 {
		for _, r := range reports {
			WriteSingleReport(w, r, verbosity)
		}
	}

	fmt.Fprintf(w, "\n Runbook Results (%d files)\n", len(reports))
	fmt.Fprintf(w, " %s\n", strings.Repeat("\u2500", 55))

	for _, r := range reports {
		icon := "\u2713"
		if r.Summary.Failed > 0 {
			icon = "\u2717"
		}
		fmt.Fprintf(w, " %s  %-42s %d/%-3d %s\n",
			icon, r.Runbook,
			r.Summary.Passed, r.Summary.Total,
			core.FormatDurationMs(r.DurationMs))

		// At v=0, show inline failure reasons since there's no detail above.
		if verbosity == 0 {
			for _, s := range r.Steps {
				if s.Status == core.StatusFailed {
					fmt.Fprintf(w, "    \u2514\u2500 Step %d: %s\n", s.Step.Number, core.StepFailReason(s))
				}
			}
		}
	}

	totalP, totalF, totalS := 0, 0, 0
	for _, r := range reports {
		totalP += r.Summary.Passed
		totalF += r.Summary.Failed
		totalS += r.Summary.Skipped
	}

	fmt.Fprintf(w, " %s\n", strings.Repeat("\u2500", 55))
	total := totalP + totalF + totalS
	fmt.Fprintf(w, " %d/%d passed", totalP, total)
	if totalF > 0 {
		fmt.Fprintf(w, "  %d failed", totalF)
	}
	if totalS > 0 {
		fmt.Fprintf(w, "  %d skipped", totalS)
	}
	fmt.Fprintln(w)
}

// writeAssertionDetails prints assertion results for a step (-v).
func writeAssertionDetails(w io.Writer, s core.StepResult) {
	// Show skip reason.
	if s.Status == core.StatusSkipped && s.Error != "" && s.Error != "manual step" {
		fmt.Fprintf(w, "          \u25CB %s\n", s.Error)
		return
	}
	if len(s.Assertions) == 0 && s.Status == core.StatusFailed {
		reason := core.StepFailReason(s)
		if reason != "" {
			fmt.Fprintf(w, "          \u2717 %s\n", reason)
		}
		return
	}
	for _, a := range s.Assertions {
		icon := "\u2713"
		if !a.Matched {
			icon = "\u2717"
		}
		label := a.Pattern
		if a.Type != "" && a.Type != core.AssertSubstring {
			label = a.Type + ": " + a.Pattern
		}
		fmt.Fprintf(w, "          %s %s\n", icon, label)
	}
}

// writeOutputSnippet prints first/last 5 lines of output (-vv).
func writeOutputSnippet(w io.Writer, label, output string) {
	output = strings.TrimSpace(output)
	if output == "" {
		return
	}
	lines := strings.Split(output, "\n")
	fmt.Fprintf(w, "          %s (%d lines):\n", label, len(lines))
	if len(lines) <= 10 {
		for _, l := range lines {
			fmt.Fprintf(w, "            %s\n", l)
		}
	} else {
		for _, l := range lines[:5] {
			fmt.Fprintf(w, "            %s\n", l)
		}
		fmt.Fprintf(w, "            ... (%d lines omitted)\n", len(lines)-10)
		for _, l := range lines[len(lines)-5:] {
			fmt.Fprintf(w, "            %s\n", l)
		}
	}
}

func plainStatusIcon(status string) string {
	switch status {
	case core.StatusPassed:
		return "\u2713"
	case core.StatusFailed:
		return "\u2717"
	case core.StatusSkipped:
		return "\u25CB"
	default:
		return "\u25CF"
	}
}

func firstFailedStepWithDebug(r core.Report) *core.StepResult {
	for i := range r.Steps {
		if r.Steps[i].Status == core.StatusFailed && r.Steps[i].Debug != nil {
			return &r.Steps[i]
		}
	}
	return nil
}
