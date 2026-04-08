---
name: mdproof
description: >-
  Use when: writing E2E, integration, or smoke tests; verifying CLI tools, APIs,
  or deployments; creating executable documentation; running existing runbook or
  proof files; or after implementing a feature to verify it works. Activate when
  the project has *_runbook.md/*_proof.md files, a mdproof.json config, or the
  user mentions mdproof/runbook/proof. Executable runbook runner — turns Markdown
  into real tests with bash execution, 6 assertion types (substring, regex,
  exit_code, jq, negation, snapshot), and source-aware failure reporting that
  points to the exact Markdown file and line.
argument-hint: "[test-description | runbook-path | 'run']"
metadata: 
  targets: [universal, claude, codex]
---

# mdproof — Executable Runbook Runner

Turn Markdown into real tests. mdproof parses `.md` files, extracts bash steps, runs them in a persistent shell session, asserts the output, and reports failures with exact Markdown file + line numbers.

## When to Use This

- User asks to write tests, E2E tests, integration tests, or smoke tests
- User asks to test a CLI tool, API, deployment, or infrastructure
- User asks to create executable documentation or runbooks
- Project contains `*_runbook.md`, `*-runbook.md`, `*_proof.md`, or `*-proof.md` files
- Project has a `mdproof.json` config file
- User says "mdproof", "runbook", or "proof"

## Start Here — Copy, Paste, Run

````markdown
# My Smoke Test

## Scope

Verify the service builds, responds to health checks, and handles resource creation.

## Steps

### Step 1: Build

```bash
go build -o /tmp/myapp ./cmd/myapp
```

Expected:

- exit_code: 0

### Step 2: Health check

```bash
curl -sf http://localhost:8080/health
```

Expected:

- exit_code: 0
- jq: .status == "ok"

### Step 3: Create resource

```bash
curl -s -X POST http://localhost:8080/items \
  -H "Content-Type: application/json" \
  -d '{"name":"test"}'
```

Expected:

- jq: .id != null
- jq: .name == "test"
- Should NOT contain error
````

Save as `*-proof.md`, then:

```bash
mdproof my-proof.md --strict=false    # local (no container)
mdproof sandbox my-proof.md           # auto-container (recommended)
```

## When It Fails — Source-Aware Reporting

mdproof points failures back to the exact Markdown file and line:

```text
FAIL runbooks/fixtures/source-aware-assert-proof.md:13 Step 1: Assertion failure
Assertion runbooks/fixtures/source-aware-assert-proof.md:13 expected output
Command runbooks/fixtures/source-aware-exit-proof.md:7-10
runbooks/fixtures/source-aware-broken.md:7: unclosed code fence
```

All three output formats carry source locations:
- **Plain text** — human-readable `file:line` in terminal output
- **JSON** — `steps[].source.heading`, `steps[].source.code_blocks[]`, `steps[].assertions[].source`
- **JUnit XML** — source locations in failure messages

**Agent workflow**: parse `--report json`, locate the failing line, edit the Markdown or fix the code, re-run.

## Quick Reference

```bash
mdproof test-proof.md                 # Run a single runbook
mdproof ./tests/                      # Run all in directory
mdproof --dry-run test-proof.md       # Parse only
mdproof -v test-proof.md              # Verbose (show assertions)
mdproof -v -v test-proof.md           # Extra verbose (show output)
mdproof -o results.json test-proof.md # JSON report to file
mdproof --report json test-proof.md   # JSON report to stdout
mdproof --report junit test-proof.md  # JUnit XML report to stdout
mdproof --report github test-proof.md # GitHub Actions annotations
mdproof --fail-fast ./tests/          # Stop on first failure
mdproof --steps 1,3 test-proof.md     # Run specific steps
mdproof --from 3 test-proof.md        # Run from step 3 onwards
mdproof --keep-failed-artifacts test-proof.md # Keep failed artifact dirs
mdproof --print-step-script test-proof.md     # Print failed step script to stderr
mdproof --print-step-env test-proof.md        # Print failed step env to stderr
mdproof -u test-proof.md              # Update snapshots
mdproof --inline README.md            # Test inline code examples
mdproof --coverage ./tests/           # Coverage analysis (no exec)
mdproof -step-setup 'rm -rf /tmp/t' test.md  # Run before each step
mdproof -step-teardown 'cleanup' test.md     # Run after each step
mdproof --isolation per-runbook ./tests/ # Isolated $HOME/$TMPDIR per runbook
mdproof --workdir /tmp/workspace tests/ # Run steps in a specific directory
mdproof sandbox tests/                # Auto-provision container and run
mdproof sandbox --image node:20 tests/ # Custom image
mdproof upgrade                       # Self-update
```

## Container Safety

mdproof defaults to **strict mode** — refuses to execute outside containers. Options to run:

1. **Sandbox mode** (recommended — auto-provisions a container):
   ```bash
   mdproof sandbox test-proof.md
   mdproof sandbox --image node:20 tests/   # custom image
   mdproof sandbox --keep --ro tests/       # keep container, read-only mount
   ```

2. **Inside a container** (manual Docker setup):
   ```bash
   docker exec $CONTAINER bash -c 'cd /workspace && mdproof test-proof.md'
   ```

3. **CLI flag** (one-off):
   ```bash
   mdproof --strict=false test-proof.md
   ```

4. **Config file** (per-project):
   ```json
   { "strict": false }
   ```

5. **Environment variable** (CI):
   ```bash
   MDPROOF_ALLOW_EXECUTE=1 mdproof test-proof.md
   ```

## Writing Runbooks

### File Naming

When given a directory, mdproof discovers: `*_runbook.md`, `*-runbook.md`, `*_proof.md`, `*-proof.md`.

### Basic Structure

````markdown
# Test Title

## Scope
Brief description.

## Steps

### Step 1: Describe what this step does

```bash
echo "hello world"
```

Expected:

- hello world

### Step 2: Check an API

```bash
curl -s http://localhost:8080/health
```

Expected:

- exit_code: 0
- jq: .status == "ok"
````

### Step Headings

Use `##` or `###` with a number: `### Step 1: Title`, `### 2. Also valid`, `### 3b. Suffix stripped`.

### Code Blocks

Only `bash`/`sh` blocks execute. Others are skipped (manual steps). No language tag defaults to `bash`. Multiple blocks per step are joined.

### Sub-Command Separator (`---`)

Use `---` on its own line within a code block to split into independent sub-commands. Each runs in its own subshell:

````markdown
### Step 1: Setup and verify

```bash
echo "create resources"
---
echo "verify resources"
```

Expected:

- create resources
- verify resources
````

Variables persist across `---` blocks within the same step — each sub-command saves its environment via EXIT trap, and the session uses `set -a` (allexport) so all assignments are automatically exported. The step's exit code is the last non-zero sub-command exit code (or 0 if all succeed). JSON report includes a `sub_commands` array with per-sub-command `exit_code`, `stdout`, `stderr`, and `command`.

Single-command steps (no `---`) behave exactly as before — no `sub_commands` field in the report.

### Persistent Session

All steps share a single bash process. Exports persist across steps:

````markdown
### Step 1: Set up

```bash
export API_URL=http://localhost:8080
export ITEM_COUNT=3
```

### Step 2: Use variables from step 1

```bash
curl -s $API_URL/items?limit=$ITEM_COUNT
```

Expected:

- jq: . | length > 0
````

**Note**: `--from N` skips earlier steps, so their exports won't exist. Use `--from` only with independently runnable steps.

## Assertions

Six types under `Expected:` — see `references/assertions-guide.md` for full details:

| Type | Syntax | Example |
|------|--------|---------|
| Substring | plain text | `- hello world` |
| Negated | `No`/`Should NOT`/`Must NOT` prefix | `- Should NOT contain error` |
| Exit code | `exit_code: N` or `!N` | `- exit_code: 0` |
| Regex | `regex:` prefix | `- regex: v\d+\.\d+` |
| jq | `jq:` prefix | `- jq: .status == "ok"` |
| Snapshot | `snapshot:` prefix | `- snapshot: api-response` |

No `Expected:` section → exit code decides (0 = pass).

**Negation matching**: Negated assertions use word boundary matching (`\b`), so `Not FAIL` matches the word "FAIL" but not "failed" or "0 failed". This differs from positive assertions which use substring matching. For maximum precision, you can still write `Not FAIL: reason` to match an exact phrase.

**Choosing wisely:**
- **JSON output** → `jq:` — precise and shows actual vs expected on failure
- **Error regression** → negated (`Should NOT contain panic`)
- **Stable exact output** → `snapshot:` + `mdproof -u` to create/update
- **Always add `exit_code: 0`** — explicit beats implicit; without it, a non-zero exit is only caught if there are no other assertions
- **Negation uses word boundaries** — `Not FAIL` matches "FAIL" but not "failed"

## Source-Aware Failures

When a runbook fails, mdproof can point back to the Markdown source:

```text
FAIL runbooks/fixtures/source-aware-assert-proof.md:13 Step 1: Assertion failure
Assertion runbooks/fixtures/source-aware-assert-proof.md:13 expected output
Command runbooks/fixtures/source-aware-exit-proof.md:7-10
runbooks/fixtures/source-aware-broken.md:7: unclosed code fence
```

`--report json` includes the same information under `steps[].source` and `steps[].assertions[].source`.

For failed runs, mdproof can also expose the exact debug artifacts:

- `--keep-failed-artifacts` preserves the failed runbook's `artifact_dir`
- with `--isolation per-runbook`, it also preserves `isolation_dir`
- `steps[].debug` carries `script_path`, `env_path`, `stdout_path`, `stderr_path`
- `steps[].debug.environment` captures `PWD`, `HOME`, and `TMPDIR`

Use `--print-step-script` and `--print-step-env` to dump the failed execution unit to `stderr` without corrupting JSON on `stdout`:

```bash
mdproof --print-step-script --print-step-env --report json failing-proof.md
mdproof --keep-failed-artifacts --report json failing-proof.md | jq '.artifact_dir, .steps[0].debug'
```

These can also be set as repo defaults in `mdproof.json`:

```json
{
  "keep_failed_artifacts": true,
  "print_step_script": false,
  "print_step_env": false
}
```

## CLI Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Parse only, don't execute |
| `--version` | Print version |
| `--report json` | JSON output to stdout |
| `--report junit` | JUnit XML output to stdout |
| `--report github` | GitHub Actions `::error` annotations to stdout |
| `-o FILE` | Write report to file (format follows `--report`) |
| `--timeout DURATION` | Per-step timeout (default: 2m) |
| `--build CMD` | Run once before all runbooks |
| `--setup CMD` | Run before each runbook |
| `--teardown CMD` | Run after each runbook |
| `--fail-fast` | Stop after first failure |
| `--strict` | Container-only execution (default: true) |
| `--steps 1,3,5` | Run only these steps |
| `--from N` | Run from step N onwards |
| `-u`, `--update-snapshots` | Update snapshot files |
| `--inline` | Parse inline test blocks |
| `--coverage` | Coverage report (no execution) |
| `--coverage-min N` | Minimum coverage score |
| `--isolation MODE` | `shared` (default) or `per-runbook` (isolated `$HOME`/`$TMPDIR`) |
| `--workdir DIR` | Working directory for step execution (supports shell expansion, e.g. `$HOME`) |
| `--keep-failed-artifacts` | Preserve failed artifact dirs for debugging |
| `--print-step-script` | Print the failed step script to `stderr` |
| `--print-step-env` | Print the failed step env snapshot to `stderr` |
| `-step-setup CMD` | Run command before each step (or `step_setup` in config) |
| `-step-teardown CMD` | Run command after each step (or `step_teardown` in config) |
| `-v` / `-vv` | Verbose / extra verbose |

## Advanced Features

For directives (timeout, retry, depends), hooks, config files, inline testing, source-aware reporting details, coverage, step filtering details, and full examples, see `references/advanced-features.md`.

## Workflow

1. **Read lessons** — if `.mdproof/lessons-learned.md` exists, check it for known gotchas and patterns
2. **Identify** what to test (CLI, API, deployment, script)
3. **Create** `.md` file with correct naming (`*-proof.md` or `*_runbook.md`)
4. **Write** steps + assertions. Apply lessons learned (e.g., prefer `jq:` for JSON, explicit `exit_code: 0`)
5. **Dry-run** first: `mdproof --dry-run my-proof.md`
6. **Execute**: `mdproof my-proof.md` (with `mdproof sandbox` or `--strict=false`)
7. **Debug** with `-v -v`, `--print-step-script`, `--print-step-env`, or `--keep-failed-artifacts` if something fails — read source-aware `file:line` output, inspect `artifact_dir` / `steps[].debug`, fix the Markdown or the code under test, re-run
8. **Learn** — after execution, record any new discoveries (see Self-Learning below)

## Self-Learning

This skill improves over time. After running runbooks, check if you learned something new and record it.

### When to record a lesson

- An assertion failed because of a **writing pattern issue** (not a real bug)
- You discovered a **better assertion type** for a use case (e.g., `jq:` instead of substring for JSON)
- A **gotcha** or edge case surprised you (e.g., `--from` skipping exports)
- You found a **reusable pattern** that should be applied to other runbooks
- An existing lesson in `.mdproof/lessons-learned.md` is **wrong or outdated**

### How to record

If `.mdproof/` does not exist, create it first:

```bash
mkdir -p .mdproof
```

Then ask the user: **"Should I add `.mdproof/` to `.gitignore`?"** (recommended — lessons are local AI learning data, not shared code).

Append to `.mdproof/lessons-learned.md` using this format:

```markdown
### [category] Short description

- **Context**: What was happening
- **Discovery**: What was learned
- **Fix**: What to do differently
- **Runbooks affected**: Which files were or should be updated
```

Categories: `assertion`, `pattern`, `gotcha`, `edge-case`, `performance`

### When to improve existing runbooks

After recording a lesson, check if it applies to existing runbooks:

1. **Search** `runbooks/` for the affected pattern (e.g., substring assertions on JSON output)
2. **Apply** the fix to affected runbooks (e.g., replace `- passed` with `- jq: .summary.failed == 0`)
3. **Verify** the improved runbook still passes: `mdproof --dry-run <file>` then execute
4. **Note** the update in the lesson's "Runbooks affected" field

Do NOT improve runbooks speculatively. Only apply lessons that are **confirmed** through actual execution experience.

## Rules

- **Correct file naming** — `*-proof.md` or `*_runbook.md` for auto-discovery
- **Use `mdproof sandbox`** for auto-container provisioning, or `--strict=false` / `MDPROOF_ALLOW_EXECUTE=1` for local runs
- **`--dry-run` first** to validate syntax
- **Stable assertions** — avoid timestamps, PIDs, non-deterministic values
- **Self-contained runbooks** — no ordering dependency between files
- **`jq:` for JSON** — more precise than substring
- **Hooks for infrastructure** — don't inline docker-compose in steps
- **`snapshot:` for stable outputs** — use `mdproof -u` to create/update
- **`--coverage` in CI** — ensure all steps have assertions
- **Explicit `exit_code: 0`** — better than implicit exit code checking
- **Use retained artifacts for hard failures** — `--keep-failed-artifacts` plus `steps[].debug` is the fastest way to inspect temp scripts, env snapshots, and captured stderr
