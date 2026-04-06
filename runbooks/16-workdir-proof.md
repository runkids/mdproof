# Working Directory (--workdir)

Validates the `--workdir` CLI flag and `workdir` config option. Steps execute in the specified directory, with shell expansion support.

## Steps

### Step 1: Create isolated environment

```bash
ssenv create test-workdir
echo "environment ready"
```

Expected:

- created: test-workdir

### Step 2: --workdir with absolute path

Steps should execute inside the specified directory. Use JSON output to verify actual pwd.

```bash
ssenv enter test-workdir -- bash -c "cd /workspace && mdproof --workdir /tmp --report json runbooks/fixtures/workdir-pwd-proof.md 2>/dev/null"
```

Expected:

- jq: .steps[0].stdout == "/tmp"
- jq: .summary.passed == 1

### Step 3: --workdir with shell expansion ($HOME)

Shell variables should expand inside the workdir value.

```bash
ssenv enter test-workdir -- bash -c "cd /workspace && mdproof --workdir \$HOME --report json runbooks/fixtures/workdir-pwd-proof.md 2>/dev/null"
```

Expected:

- jq: .steps[0].stdout != ""
- jq: .summary.passed == 1

### Step 4: --workdir with nonexistent directory aborts session

When cd fails, the session aborts before any step markers are emitted. The step gets a "did not complete" error.

```bash
ssenv enter test-workdir -- bash -c "cd /workspace && mdproof --workdir /nonexistent/path --report json runbooks/fixtures/workdir-pwd-proof.md 2>/dev/null"
```

Expected:

- jq: .steps[0].error == "step did not complete (session aborted)"

### Step 5: workdir in mdproof.json config

```bash
mkdir -p /tmp/mdproof-workdir-test
cat > /tmp/mdproof-workdir-test/mdproof.json << 'JSEOF'
{
  "workdir": "/tmp"
}
JSEOF
cat > /tmp/mdproof-workdir-test/check-proof.md << 'MDEOF'
# Check Workdir

## Steps

### Step 1: Print cwd

```bash
pwd
```

Expected:

- /tmp
MDEOF
ssenv enter test-workdir -- mdproof /tmp/mdproof-workdir-test/check-proof.md 2>&1
```

Expected:

- 1/1 passed

### Step 6: CLI --workdir overrides config workdir

```bash
cat > /tmp/mdproof-workdir-test/mdproof.json << 'JSEOF'
{
  "workdir": "/nonexistent"
}
JSEOF
cat > /tmp/mdproof-workdir-test/override-proof.md << 'MDEOF'
# Override Check

## Steps

### Step 1: Print cwd

```bash
pwd
```

Expected:

- /tmp
MDEOF
ssenv enter test-workdir -- mdproof --workdir /tmp /tmp/mdproof-workdir-test/override-proof.md 2>&1
```

Expected:

- 1/1 passed

### Step 7: --workdir with path containing spaces

```bash
mkdir -p "/tmp/mdproof workdir test"
ssenv enter test-workdir -- bash -c "cd /workspace && mdproof --workdir '/tmp/mdproof workdir test' --report json runbooks/fixtures/workdir-pwd-proof.md 2>/dev/null"
```

Expected:

- jq: .steps[0].stdout == "/tmp/mdproof workdir test"
- jq: .summary.passed == 1

### Step 8: Cleanup

```bash
rm -rf /tmp/mdproof-workdir-test "/tmp/mdproof workdir test"
ssrm test-workdir
```

Expected:

- deleted: test-workdir
