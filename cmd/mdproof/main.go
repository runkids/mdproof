package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/runkids/mdproof"
	"github.com/runkids/mdproof/internal/runner"
	"github.com/runkids/mdproof/internal/sandbox"
	"github.com/runkids/mdproof/internal/upgrade"
)

var version = "dev"

func main() {
	var (
		reportFmt           string
		dryRun              bool
		showVersion         bool
		timeout             time.Duration
		cliBuild            string
		cliSetup            string
		cliTeardown         string
		cliStepSetup        string
		cliStepTeardown     string
		failFast            bool
		outputFile          string
		cliIsolation        string
		keepFailedArtifacts bool
		printStepScript     bool
		printStepEnv        bool
		verbose             countFlag
	)

	flag.StringVar(&reportFmt, "report", "", "output format: json, junit")
	flag.BoolVar(&dryRun, "dry-run", false, "parse and classify only, don't execute")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.DurationVar(&timeout, "timeout", 0, "per-step timeout (default: 2m, or from mdproof.json)")
	flag.StringVar(&cliBuild, "build", "", "command to run once before all runbooks")
	flag.StringVar(&cliSetup, "setup", "", "command to run before each runbook")
	flag.StringVar(&cliTeardown, "teardown", "", "command to run after each runbook")
	flag.StringVar(&cliStepSetup, "step-setup", "", "command to run before each step")
	flag.StringVar(&cliStepTeardown, "step-teardown", "", "command to run after each step")
	flag.BoolVar(&failFast, "fail-fast", false, "stop after first failed step")
	flag.StringVar(&outputFile, "output", "", "write report to file")
	flag.StringVar(&outputFile, "o", "", "write report to file (shorthand)")
	flag.BoolVar(&keepFailedArtifacts, "keep-failed-artifacts", false, "keep failure artifacts for debugging")
	flag.BoolVar(&printStepScript, "print-step-script", false, "print the failed step script to stderr")
	flag.BoolVar(&printStepEnv, "print-step-env", false, "print the failed step environment to stderr")
	flag.Var(&verbose, "v", "verbosity level (-v or -v -v)")

	var (
		stepsFlag       string
		fromFlag        int
		updateSnapshots bool
		inlineMode      bool
		showCoverage    bool
		coverageMin     int
		strict          bool
	)
	flag.StringVar(&stepsFlag, "steps", "", "only run specific steps (comma-separated: 1,3,5)")
	flag.IntVar(&fromFlag, "from", 0, "run from step N onwards")
	flag.BoolVar(&updateSnapshots, "update-snapshots", false, "update snapshot files instead of comparing")
	flag.BoolVar(&updateSnapshots, "u", false, "update snapshot files (shorthand)")
	flag.BoolVar(&inlineMode, "inline", false, "parse inline test blocks from any .md file")
	flag.BoolVar(&showCoverage, "coverage", false, "show coverage report (no execution)")
	flag.IntVar(&coverageMin, "coverage-min", 0, "minimum coverage score (exit 1 if below)")
	flag.BoolVar(&strict, "strict", true, "container-only execution (use --strict=false to allow local)")
	flag.StringVar(&cliIsolation, "isolation", "", "isolation mode: shared, per-runbook")
	flag.Parse()

	if !mdproof.ValidIsolation(cliIsolation) {
		fmt.Fprintf(os.Stderr, "error: invalid --isolation value %q: must be %q or %q\n", cliIsolation, mdproof.IsolationShared, mdproof.IsolationPerRunbook)
		os.Exit(1)
	}

	if showVersion {
		fmt.Println("mdproof", version)
		os.Exit(0)
	}

	if a := flag.Args(); len(a) > 0 && a[0] == "upgrade" {
		if err := upgrade.Run(version); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if a := flag.Args(); len(a) > 0 && a[0] == "sandbox" {
		configDir := "."
		fileCfg, _ := mdproof.LoadConfig(configDir)
		exitCode, err := sandbox.Run(a[1:], fileCfg, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(exitCode)
	}

	if updateSnapshots && dryRun {
		fmt.Fprintln(os.Stderr, "error: --update-snapshots and --dry-run are mutually exclusive")
		os.Exit(1)
	}

	// Parse and validate step filter flags.
	var stepNums []int
	if stepsFlag != "" {
		if fromFlag > 0 {
			fmt.Fprintln(os.Stderr, "error: --steps and --from are mutually exclusive")
			os.Exit(1)
		}
		for _, s := range strings.Split(stepsFlag, ",") {
			s = strings.TrimSpace(s)
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 {
				fmt.Fprintf(os.Stderr, "error: invalid step number %q\n", s)
				os.Exit(1)
			}
			stepNums = append(stepNums, n)
		}
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mdproof [flags] <file.md|directory>")
		fmt.Fprintln(os.Stderr, "       mdproof sandbox [--image IMAGE] [--keep] [--ro] <file.md|directory>")
		fmt.Fprintln(os.Stderr, "       mdproof upgrade")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	target := args[0]
	var files []string
	var err error
	if inlineMode {
		files, err = resolveInlineFiles(target)
	} else {
		files, err = mdproof.ResolveFiles(target)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "no runbook files found")
		os.Exit(1)
	}

	exitCode := 0

	// Coverage mode: analyze only, no execution.
	if showCoverage {
		var entries []mdproof.CoverageEntry
		for _, file := range files {
			f, ferr := os.Open(file)
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", ferr)
				exitCode = 1
				continue
			}
			rb, perr := mdproof.ParseRunbook(f)
			f.Close()
			if perr != nil {
				fmt.Fprintf(os.Stderr, "error parsing %s: %v\n", file, perr)
				exitCode = 1
				continue
			}
			steps := mdproof.ClassifyAll(rb.Steps)
			result := mdproof.AnalyzeCoverage(steps)
			entries = append(entries, mdproof.CoverageEntry{
				File:   filepath.Base(file),
				Result: result,
			})
		}
		mdproof.WriteCoverageReport(os.Stdout, entries)
		if coverageMin > 0 {
			total := mdproof.CoverageTotalScore(entries)
			if total < coverageMin {
				fmt.Fprintf(os.Stderr, "coverage %d%% below minimum %d%%\n", total, coverageMin)
				os.Exit(1)
			}
		}
		os.Exit(exitCode)
	}

	// Load config: mdproof.json in target directory, CLI flags override.
	configDir := target
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		configDir = filepath.Dir(target)
	}
	fileCfg, err := mdproof.LoadConfig(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	strictExplicit := false
	keepFailedArtifactsExplicit := false
	printStepScriptExplicit := false
	printStepEnvExplicit := false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "strict":
			strictExplicit = true
		case "keep-failed-artifacts":
			keepFailedArtifactsExplicit = true
		case "print-step-script":
			printStepScriptExplicit = true
		case "print-step-env":
			printStepEnvExplicit = true
		}
	})
	cfg := mdproof.MergeConfig(
		fileCfg,
		cliBuild, cliSetup, cliTeardown, cliStepSetup, cliStepTeardown,
		timeout,
		strict,
		strictExplicit,
		cliIsolation,
		keepFailedArtifacts,
		keepFailedArtifactsExplicit,
		printStepScript,
		printStepScriptExplicit,
		printStepEnv,
		printStepEnvExplicit,
	)

	// Strict mode off → allow local execution.
	if !cfg.IsStrict() {
		os.Setenv("MDPROOF_ALLOW_EXECUTE", "1")
	}

	// Safety: refuse to execute commands outside a container.
	if !dryRun && !mdproof.IsContainerEnv() {
		fmt.Fprintln(os.Stderr, mdproof.ErrNotInContainer)
		os.Exit(1)
	}

	effectiveTimeout := cfg.TimeoutDuration()
	if effectiveTimeout == 0 {
		effectiveTimeout = mdproof.DefaultStepTimeout
	}

	// Run build hook once before all runbooks.
	if cfg.Build != "" && !dryRun {
		buildResult := mdproof.RunBuildHook(cfg.Build)
		if !buildResult.OK {
			fmt.Fprintf(os.Stderr, "build failed (exit %d), aborting\n", buildResult.ExitCode)
			if buildResult.Output != "" {
				fmt.Fprintln(os.Stderr, buildResult.Output)
			}
			os.Exit(1)
		}
	}

	reports, errs := runAllAndReport(files, dryRun, effectiveTimeout, cfg, buildRunOptions(cfg, stepNums, fromFlag, failFast, updateSnapshots), reportFmt, int(verbose), inlineMode)
	if errs > 0 {
		exitCode = 1
	}
	for _, r := range reports {
		if r.Summary.Failed > 0 {
			exitCode = 1
			break
		}
	}

	// Write report to file if --output is specified.
	if outputFile != "" && len(reports) > 0 {
		outF, err := os.Create(outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot write output file: %v\n", err)
			os.Exit(1)
		}
		switch reportFmt {
		case "junit":
			mdproof.WriteJUnitReport(outF, reports)
		default:
			if len(reports) == 1 {
				mdproof.WriteJSONReport(outF, reports[0])
			} else {
				mdproof.WriteJSONReports(outF, reports)
			}
		}
		if err := outF.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error: close output file: %v\n", err)
		}
	}

	os.Exit(exitCode)
}

// runFile runs a single runbook file with the given options.
func runFile(path, name string, dryRun bool, timeout time.Duration, cfg mdproof.Config, filter mdproof.RunOptions, updateSnapshots bool, inline bool) (runner.RunResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return runner.RunResult{}, err
	}
	defer f.Close()

	return runner.RunDetailed(f, name, runner.RunOptions{
		DryRun:              dryRun,
		Timeout:             timeout,
		SourcePath:          path,
		Setup:               cfg.Setup,
		Teardown:            cfg.Teardown,
		Steps:               filter.Steps,
		From:                filter.From,
		FailFast:            filter.FailFast,
		Env:                 cfg.Env,
		SnapshotUpdate:      updateSnapshots,
		RunbookDir:          filepath.Dir(path),
		Inline:              inline,
		StepSetup:           filter.StepSetup,
		StepTeardown:        filter.StepTeardown,
		KeepFailedArtifacts: filter.KeepFailedArtifacts,
		PrintStepScript:     filter.PrintStepScript,
		PrintStepEnv:        filter.PrintStepEnv,
	})
}

// resolveInlineFiles finds all .md files in a path (for inline mode).
func resolveInlineFiles(target string) ([]string, error) {
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
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, filepath.Join(target, e.Name()))
		}
	}
	return files, nil
}

func runAllAndReport(files []string, dryRun bool, timeout time.Duration, cfg mdproof.Config, filter mdproof.RunOptions, reportFmt string, verbosity int, inline bool) ([]mdproof.Report, int) {
	var reports []mdproof.Report
	var cleanups []func()
	errs := 0
	for _, file := range files {
		name := filepath.Base(file)

		// Per-runbook isolation: create temp HOME and TMPDIR.
		runCfg := cfg
		var isoDir string
		if cfg.Isolation == mdproof.IsolationPerRunbook {
			var err error
			isoDir, err = os.MkdirTemp("", "mdproof-iso-*")
			if err != nil {
				fmt.Fprintf(os.Stderr, "error creating isolation dir: %v\n", err)
				errs++
				continue
			}
			isoTmp := filepath.Join(isoDir, "tmp")
			if err := os.MkdirAll(isoTmp, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "error creating isolation tmpdir: %v\n", err)
				os.RemoveAll(isoDir)
				errs++
				continue
			}
			// Copy env map to avoid mutation across iterations.
			runCfg.Env = make(map[string]string, len(cfg.Env)+2)
			for k, v := range cfg.Env {
				runCfg.Env[k] = v
			}
			runCfg.Env["HOME"] = isoDir
			runCfg.Env["TMPDIR"] = isoTmp
		}

		runResult, err := runFile(file, name, dryRun, timeout, runCfg, filter, filter.SnapshotUpdate, inline)
		if err != nil {
			if runResult.Cleanup != nil {
				runResult.Cleanup()
			}
			if isoDir != "" {
				_ = os.RemoveAll(isoDir)
			}
			fmt.Fprintf(os.Stderr, "error running %s: %v\n", file, err)
			errs++
			continue
		}
		rpt := runResult.Report
		keepArtifacts := filter.KeepFailedArtifacts && shouldRetainArtifacts(rpt)
		if keepArtifacts {
			rpt.ArtifactDir = runResult.ArtifactDir
			if isoDir != "" {
				rpt.IsolationDir = isoDir
			}
		} else {
			if runResult.Cleanup != nil {
				cleanups = append(cleanups, runResult.Cleanup)
			}
			if isoDir != "" {
				isoDirCopy := isoDir
				cleanups = append(cleanups, func() {
					_ = os.RemoveAll(isoDirCopy)
				})
			}
		}
		reports = append(reports, rpt)
	}

	if reportFmt == "json" && len(reports) > 0 {
		if len(reports) == 1 {
			mdproof.WriteJSONReport(os.Stdout, reports[0])
		} else {
			mdproof.WriteJSONReports(os.Stdout, reports)
		}
	}

	if reportFmt == "junit" && len(reports) > 0 {
		mdproof.WriteJUnitReport(os.Stdout, reports)
	} else if reportFmt != "json" && len(reports) > 0 {
		if len(reports) > 1 {
			mdproof.WritePlainSummary(os.Stdout, reports, verbosity)
		} else {
			mdproof.WriteSingleReport(os.Stdout, reports[0], verbosity)
		}
	}

	if filter.PrintStepScript || filter.PrintStepEnv {
		printFailureDebug(os.Stderr, reports, filter)
	}
	for _, cleanup := range cleanups {
		cleanup()
	}
	return reports, errs
}

func buildRunOptions(cfg mdproof.Config, stepNums []int, fromFlag int, failFast bool, updateSnapshots bool) mdproof.RunOptions {
	return mdproof.RunOptions{
		Steps:               stepNums,
		From:                fromFlag,
		FailFast:            failFast,
		SnapshotUpdate:      updateSnapshots,
		StepSetup:           cfg.StepSetup,
		StepTeardown:        cfg.StepTeardown,
		KeepFailedArtifacts: cfg.KeepFailedArtifactsEnabled(),
		PrintStepScript:     cfg.PrintStepScriptEnabled(),
		PrintStepEnv:        cfg.PrintStepEnvEnabled(),
	}
}

func shouldRetainArtifacts(r mdproof.Report) bool {
	if r.Summary.Failed > 0 {
		return true
	}
	return r.Hooks["setup"] == mdproof.StatusFailed
}

func printFailureDebug(w io.Writer, reports []mdproof.Report, opts mdproof.RunOptions) {
	for _, rpt := range reports {
		step, ok := failedStepWithDebug(rpt)
		if !ok || step.Debug == nil {
			continue
		}
		if opts.PrintStepScript {
			fmt.Fprintf(w, "failed step script (%s step %d: %s)\n", rpt.Runbook, step.Step.Number, step.Step.Title)
			printArtifactFile(w, step.Debug.ScriptPath)
		}
		if opts.PrintStepEnv {
			fmt.Fprintf(w, "failed step environment (%s step %d: %s)\n", rpt.Runbook, step.Step.Number, step.Step.Title)
			printArtifactFile(w, step.Debug.EnvPath)
		}
		if rpt.ArtifactDir == "" {
			fmt.Fprintln(w, "rerun with --keep-failed-artifacts to preserve file paths")
		}
	}
}

func printArtifactFile(w io.Writer, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(w, "[unavailable: %v]\n", err)
		return
	}
	if _, err := w.Write(data); err != nil {
		return
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		fmt.Fprintln(w)
	}
}

func failedStepWithDebug(r mdproof.Report) (mdproof.StepResult, bool) {
	for _, step := range r.Steps {
		if step.Status == mdproof.StatusFailed && step.Debug != nil {
			return step, true
		}
	}
	return mdproof.StepResult{}, false
}

// countFlag implements flag.Value for counting repeated -v flags.
type countFlag int

func (c *countFlag) String() string { return strconv.Itoa(int(*c)) }
func (c *countFlag) Set(s string) error {
	if s == "true" || s == "" {
		*c++
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("invalid verbosity %q", s)
	}
	*c = countFlag(n)
	return nil
}
func (c *countFlag) IsBoolFlag() bool { return true }
