package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_WithBuild(t *testing.T) {
	dir := t.TempDir()
	data := `{"build": "make build", "setup": "echo setup", "timeout": "5m"}`
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build != "make build" {
		t.Errorf("build = %q, want %q", cfg.Build, "make build")
	}
}

func TestMerge_CLIBuildOverrides(t *testing.T) {
	file := Config{Build: "file-build"}
	merged := Merge(file, "cli-build", "", "", "", "", 0, true, false, "", false, false, false, false, false, false)
	if merged.Build != "cli-build" {
		t.Errorf("build = %q, want %q", merged.Build, "cli-build")
	}
}

func TestLoad_FileExists(t *testing.T) {
	dir := t.TempDir()
	data := `{"setup": "echo setup", "teardown": "echo teardown", "timeout": "5m"}`
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Setup != "echo setup" {
		t.Errorf("setup = %q, want %q", cfg.Setup, "echo setup")
	}
	if cfg.Teardown != "echo teardown" {
		t.Errorf("teardown = %q, want %q", cfg.Teardown, "echo teardown")
	}
	if cfg.Timeout != "5m" {
		t.Errorf("timeout = %q, want %q", cfg.Timeout, "5m")
	}
}

func TestLoad_NoFile(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Setup != "" || cfg.Teardown != "" || cfg.Timeout != "" {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte("{bad"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestMerge_CLIOverrides(t *testing.T) {
	file := Config{
		Setup:    "file-setup",
		Teardown: "file-teardown",
		Timeout:  "1m",
	}

	merged := Merge(file, "", "cli-setup", "", "", "", 0, true, false, "", false, false, false, false, false, false)
	if merged.Setup != "cli-setup" {
		t.Errorf("setup = %q, want %q", merged.Setup, "cli-setup")
	}
	if merged.Teardown != "file-teardown" {
		t.Errorf("teardown should keep file value, got %q", merged.Teardown)
	}
}

func TestMerge_CLITimeoutOverrides(t *testing.T) {
	file := Config{Timeout: "1m"}
	merged := Merge(file, "", "", "", "", "", 5*time.Minute, true, false, "", false, false, false, false, false, false)
	if merged.Timeout != "5m0s" {
		t.Errorf("timeout = %q, want %q", merged.Timeout, "5m0s")
	}
}

func TestIsStrict_DefaultTrue(t *testing.T) {
	cfg := Config{}
	if !cfg.IsStrict() {
		t.Error("IsStrict() should default to true when Strict is nil")
	}
}

func TestIsStrict_ConfigFalse(t *testing.T) {
	f := false
	cfg := Config{Strict: &f}
	if cfg.IsStrict() {
		t.Error("IsStrict() should return false when config sets strict=false")
	}
}

func TestMerge_CLIStrictOverridesConfig(t *testing.T) {
	f := false
	file := Config{Strict: &f}
	merged := Merge(file, "", "", "", "", "", 0, true, true, "", false, false, false, false, false, false) // CLI explicit --strict=true
	if !merged.IsStrict() {
		t.Error("CLI --strict=true should override config strict=false")
	}
}

func TestMerge_ConfigStrictNotOverriddenByDefault(t *testing.T) {
	f := false
	file := Config{Strict: &f}
	merged := Merge(file, "", "", "", "", "", 0, true, false, "", false, false, false, false, false, false) // CLI not explicit
	if merged.IsStrict() {
		t.Error("config strict=false should be preserved when CLI --strict is not explicit")
	}
}

func TestLoad_StrictFalse(t *testing.T) {
	dir := t.TempDir()
	data := `{"strict": false}`
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsStrict() {
		t.Error("config with strict=false should return IsStrict()=false")
	}
}

func TestLoadSandboxConfig(t *testing.T) {
	dir := t.TempDir()
	data := `{"sandbox":{"image":"node:20","keep":true,"ro":true}}`
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox == nil {
		t.Fatal("expected sandbox config, got nil")
	}
	if cfg.Sandbox.Image != "node:20" {
		t.Errorf("image = %q, want %q", cfg.Sandbox.Image, "node:20")
	}
	if !cfg.Sandbox.Keep {
		t.Error("keep = false, want true")
	}
	if !cfg.Sandbox.RO {
		t.Error("ro = false, want true")
	}
}

func TestLoadSandboxConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	data := `{"build":"make"}`
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox != nil {
		t.Errorf("expected nil sandbox config, got %+v", cfg.Sandbox)
	}
}

func TestLoad_StepSetup(t *testing.T) {
	dir := t.TempDir()
	data := `{"step_setup": "reset-db", "step_teardown": "dump-logs"}`
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StepSetup != "reset-db" {
		t.Errorf("step_setup = %q, want %q", cfg.StepSetup, "reset-db")
	}
	if cfg.StepTeardown != "dump-logs" {
		t.Errorf("step_teardown = %q, want %q", cfg.StepTeardown, "dump-logs")
	}
}

func TestLoad_ObservabilityDefaults(t *testing.T) {
	dir := t.TempDir()
	data := `{"keep_failed_artifacts": true, "print_step_script": true, "print_step_env": false}`
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.KeepFailedArtifactsEnabled() {
		t.Fatal("expected keep_failed_artifacts=true from config")
	}
	if !cfg.PrintStepScriptEnabled() {
		t.Fatal("expected print_step_script=true from config")
	}
	if cfg.PrintStepEnvEnabled() {
		t.Fatal("expected print_step_env=false from config")
	}
}

func TestMerge_CLIStepSetupOverrides(t *testing.T) {
	file := Config{StepSetup: "file-setup", StepTeardown: "file-teardown"}
	merged := Merge(file, "", "", "", "cli-setup", "", 0, true, false, "", false, false, false, false, false, false)
	if merged.StepSetup != "cli-setup" {
		t.Errorf("step_setup = %q, want %q", merged.StepSetup, "cli-setup")
	}
	if merged.StepTeardown != "file-teardown" {
		t.Errorf("step_teardown should keep file value, got %q", merged.StepTeardown)
	}
}

func TestMerge_ConfigStepSetupPreserved(t *testing.T) {
	file := Config{StepSetup: "file-setup"}
	merged := Merge(file, "", "", "", "", "", 0, true, false, "", false, false, false, false, false, false)
	if merged.StepSetup != "file-setup" {
		t.Errorf("step_setup = %q, want %q", merged.StepSetup, "file-setup")
	}
}

func TestMerge_CLIObservabilityOverridesConfig(t *testing.T) {
	tTrue := true
	tFalse := false
	file := Config{
		KeepFailedArtifacts: &tTrue,
		PrintStepScript:     &tTrue,
		PrintStepEnv:        &tFalse,
	}

	merged := Merge(file, "", "", "", "", "", 0, false, false, "", false, true, false, true, true, true)

	if merged.KeepFailedArtifactsEnabled() {
		t.Fatal("expected explicit CLI false to override config keep_failed_artifacts=true")
	}
	if merged.PrintStepScriptEnabled() {
		t.Fatal("expected explicit CLI false to override config print_step_script=true")
	}
	if !merged.PrintStepEnvEnabled() {
		t.Fatal("expected explicit CLI true to override config print_step_env=false")
	}
}

func TestMerge_ConfigObservabilityPreservedWhenCLINotExplicit(t *testing.T) {
	tTrue := true
	file := Config{
		KeepFailedArtifacts: &tTrue,
		PrintStepScript:     &tTrue,
	}

	merged := Merge(file, "", "", "", "", "", 0, false, false, "", false, false, false, false, false, false)

	if !merged.KeepFailedArtifactsEnabled() {
		t.Fatal("expected config keep_failed_artifacts=true to be preserved")
	}
	if !merged.PrintStepScriptEnabled() {
		t.Fatal("expected config print_step_script=true to be preserved")
	}
	if merged.PrintStepEnvEnabled() {
		t.Fatal("expected default print_step_env=false")
	}
}

func TestLoad_Isolation_PerRunbook(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(`{"isolation":"per-runbook"}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Isolation != "per-runbook" {
		t.Fatalf("expected 'per-runbook', got %q", cfg.Isolation)
	}
}

func TestLoad_Isolation_DefaultEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Empty string means "shared" (default). Callers check cfg.Isolation == "per-runbook".
	if cfg.Isolation != "" {
		t.Fatalf("expected empty (default shared), got %q", cfg.Isolation)
	}
}

func TestLoad_Isolation_ExplicitShared(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(`{"isolation":"shared"}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Isolation != "shared" {
		t.Fatalf("expected 'shared', got %q", cfg.Isolation)
	}
}

func TestLoad_Isolation_InvalidValue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mdproof.json"), []byte(`{"isolation":"full"}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid isolation value")
	}
}

func TestMerge_CLIIsolationOverrides(t *testing.T) {
	fileCfg := Config{Isolation: "shared"}
	merged := Merge(fileCfg, "", "", "", "", "", 0, false, false, "per-runbook", false, false, false, false, false, false)
	if merged.Isolation != "per-runbook" {
		t.Fatalf("expected 'per-runbook', got %q", merged.Isolation)
	}
}

func TestMerge_ConfigIsolationPreserved(t *testing.T) {
	fileCfg := Config{Isolation: "per-runbook"}
	merged := Merge(fileCfg, "", "", "", "", "", 0, false, false, "", false, false, false, false, false, false)
	if merged.Isolation != "per-runbook" {
		t.Fatalf("expected 'per-runbook', got %q", merged.Isolation)
	}
}

func TestTimeoutDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"30s", 30 * time.Second},
		{"", 0},
		{"invalid", 0},
	}
	for _, tt := range tests {
		cfg := Config{Timeout: tt.input}
		got := cfg.TimeoutDuration()
		if got != tt.want {
			t.Errorf("TimeoutDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
