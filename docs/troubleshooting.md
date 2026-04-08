# Troubleshooting

Common errors and how to fix them.

## Strict Mode: "refusing to execute outside a container"

```
mdproof: refusing to execute outside a container (strict mode)
  Runbook commands are designed to run inside a devcontainer/Docker.
```

**Cause:** mdproof defaults to `--strict=true`, which refuses to execute commands outside a container for safety.

**Fix** (pick one):

```bash
mdproof sandbox <file>           # recommended: auto-provisions a container
mdproof --strict=false <file>    # run locally (use with caution)
```

Or set it in `mdproof.json`:

```json
{ "strict": false }
```

Or set the environment variable:

```bash
export MDPROOF_ALLOW_EXECUTE=1
```

## jq: "jq is not installed"

```
jq is not installed (required for jq: assertions)
```

**Cause:** `jq:` assertions require the `jq` binary on your PATH.

**Fix:**

```bash
# macOS
brew install jq

# Debian / Ubuntu
apt-get install jq
```

Or use `mdproof sandbox` — the sandbox container includes jq by default.

## jq: assertion fails but output looks correct

```
FAIL  jq failed: ...
```

**Common causes:**

1. **Output is not valid JSON.** jq requires strict JSON input. Check for trailing text, ANSI escape codes, or mixed output.
   ```bash
   # debug: pipe your command output to jq manually
   curl -s http://localhost:8080/api | jq .
   ```

2. **Expression syntax error.** jq uses its own expression syntax — string comparisons need quotes:
   ```markdown
   - jq: .status == "ok"      ✓  (quotes around "ok")
   - jq: .status == ok        ✗  (unquoted string)
   ```

3. **Null vs missing.** `.field != null` passes when the field exists with any value. `.field` alone passes when the value is truthy.

## Snapshots: "snapshot mismatch"

```
snapshot mismatch:
  - expected line
  + actual line
```

**Cause:** The command output no longer matches the stored snapshot.

**Fix:**

- If the change is intentional, update the snapshot:
  ```bash
  mdproof -u <file>
  ```
- If unexpected, check what changed in your command output.

**Where snapshots are stored:** `__snapshots__/<runbook-name>.snap` next to your runbook file. Each snapshot is keyed by name — names must be unique within a runbook.

**Lifecycle:**
1. First run → snapshot file is created automatically
2. Later runs → output is compared against stored snapshot
3. Run with `-u` → snapshot is overwritten with current output

## Timeouts: "step timed out"

**Cause:** A step exceeded the per-step timeout (default: 2 minutes).

**Fix:**

```bash
mdproof --timeout 5m <file>
```

Or use a per-step directive:

```markdown
<!-- runbook: timeout=5m -->
```

## Steps skipped after setup failure

```
 ⊘  Step 1  (skipped: setup failed)
```

**Cause:** The `--setup` hook failed, so all steps in that runbook are skipped.

**Fix:** Check your setup command. Run it manually to debug:

```bash
# whatever you passed to --setup
docker-compose up -d && sleep 2
```

## "no runbooks found"

**Cause:** mdproof looks for files matching `*_runbook.md`, `*-runbook.md`, `*_proof.md`, or `*-proof.md` when given a directory.

**Fix:** Either rename your file to match the pattern, or pass the file path directly:

```bash
mdproof sandbox my-tests.md          # explicit file — any name works
mdproof sandbox ./runbooks/           # directory — needs matching pattern
```

## Exit code assertions

```
FAIL  expected exit code 0, got 1
```

**Tips:**

- When a step has no `Expected:` section, mdproof checks for exit code 0 by default.
- Use `exit_code: N` to expect a non-zero exit code (e.g., testing error paths).
- Sub-commands (`---` separated) each have their own exit code. The step exit code comes from the **last** sub-command.

## Still stuck?

1. Use `--dry-run` to check parsing without executing:
   ```bash
   mdproof --dry-run <file>
   ```

2. Use verbose mode for more detail:
   ```bash
   mdproof -v <file>       # verbose
   mdproof -v -v <file>    # extra verbose
   ```

3. Check the [CLI Reference](cli-reference.md) for all available flags.

4. File an issue: [github.com/runkids/mdproof/issues](https://github.com/runkids/mdproof/issues)
