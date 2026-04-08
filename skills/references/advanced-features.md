# Advanced Features Reference

## Directives

HTML comment directives control per-step behavior. Place them before the code block.

### Per-Step Timeout

In the step title:
```markdown
### Step 5: Slow build (timeout: 10m)
```

Or via HTML comment:
```markdown
<!-- runbook: timeout=30s -->
```

### Retry on Failure

```markdown
<!-- runbook: retry=3 delay=5s -->
```

Retries up to 3 times with 5s delay between attempts.

### Step Dependencies

```markdown
<!-- runbook: depends=2 -->
```

Skips this step if step 2 failed.

### Combined

```markdown
### Step 4: Wait for service

<!-- runbook: timeout=2m retry=5 delay=10s depends=3 -->

```bash
curl -sf http://localhost:8080/ready
```
```

## Lifecycle Hooks

### Build Hook

Runs once before all runbooks. Failure aborts everything:

```bash
mdproof --build "make build" ./tests/
```

### Setup Hook

Runs before each runbook. Failure skips all steps in that runbook:

```bash
mdproof --setup "docker-compose up -d" ./tests/
```

### Teardown Hook

Runs after each runbook, always, even on failure:

```bash
mdproof --teardown "docker-compose down" ./tests/
```

### All Together

```bash
mdproof \
  --build "make build" \
  --setup "docker-compose up -d && make seed" \
  --teardown "docker-compose down -v" \
  ./tests/
```

Setup and teardown share the session with steps (env vars persist). Build runs as a separate process.

## Per-Step Setup/Teardown

Distinct from lifecycle hooks (`--setup`/`--teardown` which run per-runbook), these run before/after **each step**:

```bash
mdproof -step-setup 'rm -rf /tmp/test-state && mkdir -p /tmp/test-state' test.md
mdproof -step-teardown 'echo step done' test.md
mdproof -step-setup 'reset-db' -step-teardown 'dump-logs' test.md
```

Also configurable in `mdproof.json` (CLI flags override config values):

```json
{
  "step_setup": "reset-db",
  "step_teardown": "dump-logs"
}
```

**Behavior:**
- **Setup failure** → step is marked `failed`, step body is skipped
- **Teardown failure** → informational only, step status unaffected
- Setup/teardown stdout is **not** mixed into step stdout
- JSON report includes `step_setup` and `step_teardown` objects with `exit_code`, `stdout`, `stderr`
- When neither flag is provided, no `step_setup`/`step_teardown` fields appear in the report

**With retry:** When a step has `<!-- runbook: retry=N -->` and step-setup/teardown are active, each retry attempt runs the full cycle: setup → body → teardown.

**Flag ordering:** Go's `flag` package requires all flags before positional arguments:
```bash
# CORRECT
mdproof --report json -step-setup 'echo hi' test.md

# WRONG — flags after file path are silently ignored
mdproof test.md --report json -step-setup 'echo hi'
```

## Sub-Command Separator (`---`)

Split a single step's code block into independent sub-commands using `---`:

````markdown
### Step 1: Multi-phase verification

```bash
echo "phase 1: create"
---
echo "phase 2: verify"
---
echo "phase 3: cleanup"
```
````

**Execution model:**
- Each `---` block runs in its own subshell `(...)` within the session script
- Variables persist across sub-commands within the same step — every sub-command saves `export -p` to the env file via EXIT trap, and the next sub-command sources it on entry. The session uses `set -a` (allexport), so all assignments are automatically exported
- The step's overall exit code is the last non-zero sub-command exit code (or 0 if all succeed)
- `--fail-fast` applies: if a sub-command fails and fail-fast is on, remaining sub-commands in that step are skipped

**JSON report:** Steps with `---` include a `sub_commands` array:
```json
{
  "sub_commands": [
    { "command": "echo phase 1", "exit_code": 0, "stdout": "phase 1" },
    { "command": "echo phase 2", "exit_code": 0, "stdout": "phase 2" }
  ]
}
```

Single-command steps (no `---`) do not have a `sub_commands` field — backward compatible.

**Plain text report:** At verbosity 0, the first failing sub-command's stderr is shown. At `-v -v`, per-sub-command details are printed.

**JUnit report:** Sub-command exit codes and stderr are appended to the failure body.

## Report Output

### JSON stdout in directory mode

When running multiple runbooks with `--report json`:

- **Single file** → one JSON object on stdout (`jq .summary` works)
- **Directory / multiple files** → JSON array on stdout (`jq '.[].summary'`)
- **`-o FILE`** → always writes a JSON array to the file, regardless of count

```bash
# Single file
mdproof --report json test-proof.md | jq .summary

# Directory
mdproof --report json ./tests/ | jq '.[].summary'
```

JSON reports also expose Markdown source metadata:

```json
{
  "steps": [
    {
      "source": {
        "heading": { "start": { "line": 5 }, "end": { "line": 5 } },
        "code_blocks": [
          { "start": { "line": 7 }, "end": { "line": 9 } }
        ]
      },
      "assertions": [
        {
          "pattern": "expected output",
          "source": { "start": { "line": 13 }, "end": { "line": 13 } }
        }
      ]
    }
  ]
}
```

### Source-Aware Failures

mdproof preserves Markdown source ranges for step headings, code blocks, and assertion bullets. That data is surfaced in plain text, JSON, and JUnit output:

```text
FAIL runbooks/fixtures/source-aware-assert-proof.md:13 Step 1: Assertion failure
Assertion runbooks/fixtures/source-aware-assert-proof.md:13 expected output
Command runbooks/fixtures/source-aware-exit-proof.md:7-10
runbooks/fixtures/source-aware-broken.md:7: unclosed code fence
```

### Failure Observability

For harder failures where source locations are not enough, mdproof can preserve and print the failed execution artifacts:

```bash
mdproof --keep-failed-artifacts failing-proof.md
mdproof --print-step-script --print-step-env failing-proof.md
mdproof --print-step-script --print-step-env --report json failing-proof.md
```

**Retained artifacts:**
- `artifact_dir` points to the executor session temp dir
- `isolation_dir` is also retained when `--isolation per-runbook` is active
- `steps[].debug` identifies the failed execution unit with:
  - `script_path`
  - `env_path`
  - `stdout_path`
  - `stderr_path`
- `steps[].debug.environment` captures `PWD`, `HOME`, and `TMPDIR`

**Targeting behavior:**
- step-setup failures point at `step_<n>_setup.*`
- sub-command failures point at the first failing `step_<n>_sub_<i>.*`
- assertion failures with exit code 0 still point at the main `step_<n>.*` artifacts
- teardown-only failures are informational and do not trigger artifact retention

**Printing behavior:**
- `--print-step-script` dumps the failed execution unit's script to `stderr`
- `--print-step-env` dumps the failed execution unit's env snapshot to `stderr`
- these flags do not imply keep; if artifacts are not retained, mdproof prints a rerun hint for `--keep-failed-artifacts`
- because printing goes to `stderr`, `--report json` remains machine-readable on `stdout`

**Config defaults:** these can be enabled in `mdproof.json`:

```json
{
  "keep_failed_artifacts": true,
  "print_step_script": false,
  "print_step_env": false
}
```

## Configuration File

Create `mdproof.json` in the runbook directory:

```json
{
  "build": "make build",
  "setup": "docker-compose up -d",
  "teardown": "docker-compose down",
  "step_setup": "rm -rf /tmp/test-state && mkdir -p /tmp/test-state",
  "step_teardown": "echo step done",
  "keep_failed_artifacts": true,
  "print_step_script": false,
  "print_step_env": false,
  "timeout": "5m",
  "strict": false,
  "isolation": "per-runbook",
  "workdir": "/tmp/workspace",
  "env": {
    "DATABASE_URL": "postgres://localhost:5432/test",
    "LOG_LEVEL": "debug"
  }
}
```

CLI flags override config values. `strict` defaults to `true` if not set.

### Isolation Modes

Control whether runbooks share `$HOME` and `$TMPDIR`:

| Value | Behavior |
|-------|----------|
| `"shared"` (default) | All runbooks share the host `$HOME` and `$TMPDIR` |
| `"per-runbook"` | Each runbook gets a fresh temp dir as `$HOME` with `$TMPDIR` under `$HOME/tmp` — cleaned up after each runbook |

```bash
mdproof --isolation per-runbook ./tests/
```

Or in `mdproof.json`:

```json
{ "isolation": "per-runbook" }
```

**Behavior details:**
- Build hook (`--build`) runs once before all runbooks in the original environment — not affected by isolation
- Setup/teardown hooks inherit the isolated `$HOME`/`$TMPDIR`
- `$HOME` starts empty — use setup hooks to create `~/.config/` etc. if needed
- Invalid values (anything other than `shared` or `per-runbook`) produce an error at config load time
- CLI `--isolation` overrides the config file value

### Sandbox Configuration

Configure sandbox defaults in `mdproof.json`:

```json
{
  "sandbox": {
    "image": "node:20",
    "keep": false,
    "ro": false
  }
}
```

| Field | Description |
|-------|-------------|
| `image` | Container image (default: `debian:bookworm-slim`) |
| `keep` | Don't auto-remove container (default: `false`) |
| `ro` | Mount workspace read-only (default: `false`) |

CLI `--image`, `--keep`, `--ro` flags override config values.

## Inline Testing

Test code examples in any Markdown file (READMEs, docs, tutorials). Wrap testable blocks with HTML comment markers:

````markdown
<!-- mdproof:start -->
```bash
curl -s http://localhost:8080/health
```

Expected:

- jq: .status == "ok"
<!-- mdproof:end -->
````

Run with `--inline`:

```bash
mdproof --inline README.md
mdproof --inline ./docs/  # scans all .md files
```

Steps are auto-numbered. Nested/unclosed markers produce errors.

## Coverage Analysis

Static analysis of assertion coverage (no execution):

```bash
mdproof --coverage ./tests/
mdproof --coverage --coverage-min 80 ./tests/  # CI gate
```

Shows which steps lack assertions, total coverage score, and warns about low assertion type diversity.

## Step Filtering

### Run specific steps

```bash
mdproof --steps 1,3,5 test-proof.md
```

Only the selected steps execute; others show as skipped. **Note**: the summary counts all steps in the runbook, not just selected ones. For example, `--steps 1` on a 3-step runbook shows `1/3 passed  2 skipped`.

### Run from step N

```bash
mdproof --from 3 test-proof.md
```

Steps before N are skipped. **Warning**: because all steps share a persistent session, `--from N` skips earlier steps' exports. If step N depends on variables set by earlier steps, it will fail. Use `--from` only with independently runnable steps.

## Full Examples

### Example: Testing a CLI Tool

````markdown
# CLI Smoke Test

## Scope
Verify the myapp CLI builds and responds to basic commands.

## Steps

### Step 1: Build

```bash
go build -o /tmp/myapp ./cmd/myapp
```

Expected:

- exit_code: 0

### Step 2: Help flag

```bash
/tmp/myapp --help
```

Expected:

- usage
- Should NOT contain panic

### Step 3: Version

```bash
/tmp/myapp --version
```

Expected:

- regex: v\d+\.\d+

### Step 4: Process input

```bash
echo '{"name":"test"}' | /tmp/myapp process
```

Expected:

- jq: .status == "ok"
- jq: .name == "test"
- No error
````

### Example: Testing an API

````markdown
# API Integration Test

## Steps

### Step 1: Start server

```bash
./server &
sleep 2
curl -sf http://localhost:8080/health
```

Expected:

- exit_code: 0

### Step 2: Create resource

```bash
curl -s -X POST http://localhost:8080/items \
  -H "Content-Type: application/json" \
  -d '{"name":"test-item"}'
```

Expected:

- jq: .id != null
- jq: .name == "test-item"

### Step 3: List resources

```bash
curl -s http://localhost:8080/items
```

Expected:

- jq: . | length >= 1
- jq: .[0].name == "test-item"

### Step 4: Cleanup

```bash
kill %1 2>/dev/null || true
```

Expected:

- exit_code: 0
````

### Example: CI Integration

```yaml
# GitHub Actions — JSON report
- name: Run mdproof tests
  env:
    MDPROOF_ALLOW_EXECUTE: "1"
  run: mdproof --fail-fast -o results.json ./tests/

# GitHub Actions — JUnit XML (native test summary)
- name: Run mdproof tests
  env:
    MDPROOF_ALLOW_EXECUTE: "1"
  run: mdproof --report junit --fail-fast -o results.xml ./tests/

# GitHub Actions — inline PR annotations
- name: Run mdproof with annotations
  env:
    MDPROOF_ALLOW_EXECUTE: "1"
  run: mdproof --report github ./tests/
```
