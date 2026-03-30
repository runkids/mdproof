# CLI Reference

```
mdproof [flags] <file.md|directory>
```

When given a directory, mdproof finds files matching `*_runbook.md`, `*-runbook.md`, `*_proof.md`, or `*-proof.md`.

## Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Parse and classify only, don't execute |
| `--version` | Print version and exit |
| `--report json` | Output as JSON (single file: object, directory: array) |
| `--report junit` | Output as JUnit XML |
| `--output FILE`, `-o FILE` | Write report to file (format follows `--report`) |
| `--timeout DURATION` | Per-step timeout (default: 2m) |
| `--build CMD` | Build hook: run once before all runbooks |
| `--setup CMD` | Setup hook: run before each runbook |
| `--teardown CMD` | Teardown hook: run after each runbook |
| `--fail-fast` | Stop after first failed step |
| `--strict` | Container-only execution (default: true, use `--strict=false` to allow local) |
| `--steps 1,3,5` | Only run specific steps |
| `--from N` | Run from step N onwards |
| `--update-snapshots`, `-u` | Update snapshot files instead of comparing |
| `--inline` | Parse inline test blocks from any `.md` file |
| `--coverage` | Show coverage report (no execution) |
| `--coverage-min N` | Minimum coverage score (exit 1 if below) |
| `--isolation MODE` | `shared` (default) or `per-runbook` (isolated `$HOME`/`$TMPDIR`) |
| `--keep-failed-artifacts` | Preserve failed runbook artifact directories for debugging |
| `--print-step-script` | Print the failed step script to `stderr` |
| `--print-step-env` | Print the failed step environment snapshot to `stderr` |
| `-step-setup CMD` | Run command before each step |
| `-step-teardown CMD` | Run command after each step |
| `-v` | Show assertion details |
| `-vv` | Show assertions + stdout/stderr |

## Subcommands

| Command | Description |
|---------|-------------|
| `sandbox [flags] <file\|dir>` | Auto-provision a container and run inside it |
| `upgrade` | Self-update to the latest release |

### Sandbox Mode

Auto-provision a Docker (or Apple) container, cross-compile mdproof for the target platform, detect dependencies from runbook code blocks, and execute inside the container — all in one command:

```bash
mdproof sandbox tests/
mdproof sandbox --image node:20 api-proof.md
mdproof sandbox --keep --ro tests/   # keep container, read-only mount
```

| Flag | Description |
|------|-------------|
| `--image IMAGE` | Container image (default: `debian:bookworm-slim`) |
| `--keep` | Don't auto-remove container after exit |
| `--ro` | Mount workspace read-only |

Runtime detection: prefers Apple containers on macOS arm64 (if available), falls back to Docker. Override with `MDPROOF_RUNTIME=docker` or `MDPROOF_RUNTIME=apple`.

## Examples

```bash
# Auto-provision a container and run
mdproof sandbox deploy-proof.md

# Run a single runbook
mdproof deploy-proof.md

# Run all runbooks in a directory
mdproof ./runbooks/

# Dry-run to validate syntax
mdproof --dry-run deploy-proof.md

# Run with verbose output showing assertions
mdproof -v deploy-proof.md

# Extra verbose: assertions + stdout/stderr
mdproof -v -v deploy-proof.md

# Run specific steps only
mdproof --steps 1,3 deploy-proof.md

# Run from step 5 onwards
mdproof --from 5 deploy-proof.md

# Fail fast and output JSON
mdproof --fail-fast --report json deploy-proof.md

# Save JSON report to file
mdproof -o results.json deploy-proof.md

# Full lifecycle: build → setup → steps → teardown
mdproof \
  --build "make build" \
  --setup "make seed" \
  --teardown "make clean" \
  deploy-proof.md

# Per-step hooks
mdproof -step-setup 'rm -rf /tmp/test-*' -step-teardown 'echo done' deploy-proof.md

# Per-runbook isolation (fresh $HOME/$TMPDIR per runbook)
mdproof --isolation per-runbook ./runbooks/

# Retain failed artifacts and show debug paths
mdproof --keep-failed-artifacts runbooks/fixtures/failing-proof.md

# Print failed step script and env to stderr while keeping JSON on stdout
mdproof --print-step-script --print-step-env --report json runbooks/fixtures/failing-proof.md

# Update snapshots after intentional changes
mdproof -u deploy-proof.md

# Coverage report
mdproof --coverage ./runbooks/

# Coverage gate in CI
mdproof --coverage --coverage-min 80 ./runbooks/

# Test inline code examples in docs
mdproof --inline README.md

```

## Failure Output

When a runbook fails, mdproof includes the Markdown source location when available. If you rerun with `--keep-failed-artifacts`, the plain-text summary also shows:

- the retained `artifact dir`
- the retained `isolation dir` when `--isolation per-runbook` is active
- the failed step's script/env/stdout/stderr paths
- inline `PWD`, `HOME`, and `TMPDIR`

`--print-step-script` and `--print-step-env` print the failed step script or env snapshot to `stderr`. This keeps `--report json` valid on `stdout`.

Example:

```text
FAIL runbooks/fixtures/source-aware-assert-proof.md:13 Step 1: Assertion failure
Assertion runbooks/fixtures/source-aware-assert-proof.md:13 expected output
artifact dir: /tmp/mdproof-session-123
script path: /tmp/mdproof-session-123/step_1.sh
env path: /tmp/mdproof-session-123/step_1_env
Command runbooks/fixtures/source-aware-exit-proof.md:7-10
runbooks/fixtures/source-aware-broken.md:7: unclosed code fence
```

`--report json` includes the same information under `steps[].source`, `steps[].assertions[].source`, `environment`, `artifact_dir`, `isolation_dir`, and `steps[].debug`.
