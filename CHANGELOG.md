# Changelog

## [0.0.8] - 2026-04-06

### New Features

- **Working directory** — `--workdir DIR` sets the working directory for all step execution. Supports shell expansion (e.g., `$HOME`). Also configurable in `mdproof.json` via `workdir`. CLI flag overrides config.
  ```bash
  mdproof --workdir /tmp/workspace deploy-proof.md
  mdproof --workdir '$HOME/project' test-proof.md
  ```
  ```json
  { "workdir": "/tmp/workspace" }
  ```

### Bug Fixes

- **Working directory not applied** — fixed `runFile()` not threading `Workdir` from `RunOptions` to the executor, which silently ignored `--workdir`
- **Shell quoting for workdir** — the generated `cd` command now uses double quotes, preventing breakage with paths containing spaces and protecting against shell injection while preserving `$VAR` expansion

## [0.0.7] - 2026-03-31

### New Features

- **Retained failure artifacts** — `--keep-failed-artifacts` now preserves the failed runbook's executor artifact directory, and also keeps the isolated `$HOME` / `$TMPDIR` directory when `--isolation per-runbook` is active. You can enable it from the CLI or set `keep_failed_artifacts` in `mdproof.json`. The plain-text failure summary points directly to the retained script, env, stdout, and stderr paths so you can inspect the exact failed step without re-deriving temp locations.
  ```bash
  mdproof --keep-failed-artifacts runbooks/fixtures/failing-proof.md
  mdproof --isolation per-runbook --keep-failed-artifacts runbooks/fixtures/failing-proof.md
  ```

- **Failed-step script and env printing** — `--print-step-script` and `--print-step-env` dump the actual failed step script or captured execution environment to `stderr`, which makes debugging wrappers, temp files, and execution context much faster while keeping `--report json` clean on `stdout`. These defaults can also live in `mdproof.json` via `print_step_script` and `print_step_env`.
  ```bash
  mdproof --print-step-script --print-step-env runbooks/fixtures/failing-proof.md
  mdproof --print-step-script --print-step-env --report json runbooks/fixtures/failing-proof.md
  ```
  ```bash
  mdproof --report json runbooks/fixtures/failing-proof.md | jq '.artifact_dir, .steps[0].debug'
  ```

## [0.0.6] - 2026-03-13

### New Features

- **Source-aware failure reporting** — failed assertions, command exits, and parser errors now point to the exact Markdown file and line number. Works in plain text, JSON (`steps[].source`, `assertions[].source`), and JUnit output.
  ```text
  FAIL deploy-proof.md:13 Step 1: Assertion failure
  Assertion deploy-proof.md:13 expected output
  Command deploy-proof.md:7-10
  ```
  ```bash
  mdproof --report json test.md | jq '.steps[0].source.heading.start.line'
  ```

### Breaking Changes

- **`--watch` flag removed** — watch mode has been removed to sharpen product focus on executable runbooks, docs verification, and sandboxed smoke tests. Use an external file watcher (e.g., `entr`, `watchexec`) if you need re-run-on-change behavior.

## [0.0.5] - 2026-03-12

### New Features

- **Per-runbook isolation** — `--isolation per-runbook` gives each runbook a fresh `$HOME` and `$TMPDIR`, preventing cross-runbook pollution in directory mode. Also configurable in `mdproof.json` via `isolation`.
  ```bash
  mdproof --isolation per-runbook ./tests/
  ```
  ```json
  { "isolation": "per-runbook" }
  ```

- **Sub-command variable persistence** — variables now persist across `---` blocks within the same step. Each sub-command saves its environment via EXIT trap, so `export VAR=value` (or any assignment, since `set -a` is active) in block 1 is visible in block 2. This reverses the v0.0.4 breaking change — `---` blocks are now isolated subshells that still share environment state.

### Bug Fixes

- **JSON array output for directory mode** — `--report json` stdout now emits a valid JSON array (`[{...}, {...}]`) when running multiple runbooks, instead of streaming independent objects. Single-file mode still outputs a single JSON object.
  ```bash
  mdproof --report json ./tests/ | jq '.[].summary'   # directory mode
  mdproof --report json test.md  | jq '.summary'      # single file
  ```

## [0.0.4] - 2026-03-12

### New Features

- **Per-step setup/teardown** — `-step-setup` and `-step-teardown` CLI flags run a command before/after each step. Setup failure marks the step as failed and skips the body; teardown failure is informational only. Also configurable in `mdproof.json` via `step_setup` / `step_teardown`.
  ```bash
  mdproof -step-setup 'rm -rf /tmp/test-*' test.md
  mdproof -step-teardown 'echo cleanup' test.md
  ```
  ```json
  { "step_setup": "reset-db", "step_teardown": "dump-logs" }
  ```

- **Sub-command granular report** — steps with `---` separators now execute each block independently in its own subshell. The JSON report includes a `sub_commands` array with per-sub-command `exit_code`, `stdout`, `stderr`, and `command`. Plain text and JUnit reporters surface sub-command failure details.

### Breaking Changes

- **`---` separated blocks now run in independent subshells.** Previously, multiple code blocks within a step (delimited by `---`) ran as a single concatenated command. They now run in separate `(...)` subshells. Variables still persist across blocks (reversed in v0.0.5).

## [0.0.3] - 2026-03-11

### New Features

- **JUnit XML output** — `--report junit` produces JUnit XML for native CI test result display (GitHub Actions, GitLab CI, Jenkins)
  ```bash
  mdproof --report junit tests/          # stdout
  mdproof --report junit -o report.xml tests/  # file output
  ```

## [0.0.2] - 2026-03-11

### Bug Fixes

- **Negated assertion matching** — negated patterns (`Should NOT contain FAIL`) now use word boundary matching, so `FAIL` no longer falsely matches `failed` or `0 failed`

## [0.0.1] - 2026-03-11

Initial release.

- **Markdown-native test runner** — write tests as Markdown, run them as real tests
- **Persistent bash sessions** — env vars and exports flow across steps
- **Five assertion types** — substring, exit_code, regex, jq, snapshot
- **Negated assertions** — `Should NOT contain`, `No error`, `Must NOT match`
- **Snapshot testing** — `snapshot:` assertions with `--update-snapshots` / `-u`
- **Inline testing** — `--inline` mode tests code examples in any `.md` file
- **Coverage analysis** — `--coverage` and `--coverage-min` for CI gating
- **Watch mode** — `--watch` re-runs on file changes (removed in v0.0.6)
- **Sandbox mode** — `mdproof sandbox` auto-provisions a container for safe execution
- **Step filtering** — `--steps 1,3,5` and `--from N`
- **Lifecycle hooks** — `--build`, `--setup`, `--teardown`
- **JSON output** — `--report json` and `-o` for programmatic consumption
- **Container-first safety** — `--strict` (default) refuses execution outside containers
- **Self-update** — `mdproof upgrade`
