package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ConfigFileName is the conventional name for directory-level runbook config.
const ConfigFileName = "mdproof.json"

// Isolation mode constants.
const (
	IsolationShared     = "shared"
	IsolationPerRunbook = "per-runbook"
)

// ValidIsolation reports whether s is a recognised isolation mode (or empty).
func ValidIsolation(s string) bool {
	return s == "" || s == IsolationShared || s == IsolationPerRunbook
}

// SandboxConfig holds settings for the sandbox subcommand.
type SandboxConfig struct {
	Image string `json:"image,omitempty"` // container image (default: debian:bookworm-slim)
	Keep  bool   `json:"keep,omitempty"`  // don't auto-remove container
	RO    bool   `json:"ro,omitempty"`    // mount workspace read-only
}

// Config holds lifecycle hooks and defaults for runbook execution.
type Config struct {
	Build               string            `json:"build,omitempty"`                 // command to run once before all runbooks
	Setup               string            `json:"setup,omitempty"`                 // command to run before each runbook
	Teardown            string            `json:"teardown,omitempty"`              // command to run after each runbook
	StepSetup           string            `json:"step_setup,omitempty"`            // command to run before each step
	StepTeardown        string            `json:"step_teardown,omitempty"`         // command to run after each step
	KeepFailedArtifacts *bool             `json:"keep_failed_artifacts,omitempty"` // preserve failed artifact dirs
	PrintStepScript     *bool             `json:"print_step_script,omitempty"`     // print failed step script to stderr
	PrintStepEnv        *bool             `json:"print_step_env,omitempty"`        // print failed step env snapshot to stderr
	Timeout             string            `json:"timeout,omitempty"`               // per-step timeout (e.g., "5m")
	Env                 map[string]string `json:"env,omitempty"`                   // environment variables seeded into all steps
	Strict              *bool             `json:"strict,omitempty"`                // container-only execution (default: true)
	Sandbox             *SandboxConfig    `json:"sandbox,omitempty"`               // sandbox subcommand settings
	Isolation           string            `json:"isolation,omitempty"`             // "shared" (default) | "per-runbook"
	Workdir             string            `json:"workdir,omitempty"`              // working directory for step execution (supports shell expansion, e.g. $HOME)
}

// TimeoutDuration parses the timeout string into a time.Duration.
// Returns zero if empty or invalid.
func (c Config) TimeoutDuration() time.Duration {
	if c.Timeout == "" {
		return 0
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 0
	}
	return d
}

// Load reads an mdproof.json from the given directory.
// Returns an empty config (no error) if the file doesn't exist.
func Load(dir string) (Config, error) {
	path := filepath.Join(dir, ConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if !ValidIsolation(cfg.Isolation) {
		return Config{}, fmt.Errorf("invalid isolation value %q: must be %q or %q", cfg.Isolation, IsolationShared, IsolationPerRunbook)
	}
	return cfg, nil
}

// Merge applies CLI flag overrides on top of file-based config.
// CLI flags take precedence when non-empty. strictExplicit indicates
// whether --strict was explicitly passed on the command line.
func Merge(
	file Config,
	cliBuild, cliSetup, cliTeardown, cliStepSetup, cliStepTeardown string,
	cliTimeout time.Duration,
	cliStrict bool,
	strictExplicit bool,
	cliIsolation string,
	cliKeepFailedArtifacts bool,
	keepFailedArtifactsExplicit bool,
	cliPrintStepScript bool,
	printStepScriptExplicit bool,
	cliPrintStepEnv bool,
	printStepEnvExplicit bool,
	cliWorkdir string,
) Config {
	merged := file
	if cliBuild != "" {
		merged.Build = cliBuild
	}
	if cliSetup != "" {
		merged.Setup = cliSetup
	}
	if cliTeardown != "" {
		merged.Teardown = cliTeardown
	}
	if cliStepSetup != "" {
		merged.StepSetup = cliStepSetup
	}
	if cliStepTeardown != "" {
		merged.StepTeardown = cliStepTeardown
	}
	if cliTimeout != 0 {
		merged.Timeout = cliTimeout.String()
	}
	if strictExplicit {
		merged.Strict = &cliStrict
	}
	if cliIsolation != "" {
		merged.Isolation = cliIsolation
	}
	if keepFailedArtifactsExplicit {
		merged.KeepFailedArtifacts = boolPtr(cliKeepFailedArtifacts)
	}
	if printStepScriptExplicit {
		merged.PrintStepScript = boolPtr(cliPrintStepScript)
	}
	if printStepEnvExplicit {
		merged.PrintStepEnv = boolPtr(cliPrintStepEnv)
	}
	if cliWorkdir != "" {
		merged.Workdir = cliWorkdir
	}
	return merged
}

// IsStrict returns the effective strict mode value.
// Default is true if not set in config or CLI.
func (c Config) IsStrict() bool {
	if c.Strict == nil {
		return true
	}
	return *c.Strict
}

// KeepFailedArtifactsEnabled returns the effective keep-failed-artifacts value.
func (c Config) KeepFailedArtifactsEnabled() bool {
	return c.KeepFailedArtifacts != nil && *c.KeepFailedArtifacts
}

// PrintStepScriptEnabled returns the effective print-step-script value.
func (c Config) PrintStepScriptEnabled() bool {
	return c.PrintStepScript != nil && *c.PrintStepScript
}

// PrintStepEnvEnabled returns the effective print-step-env value.
func (c Config) PrintStepEnvEnabled() bool {
	return c.PrintStepEnv != nil && *c.PrintStepEnv
}

func boolPtr(v bool) *bool {
	return &v
}
