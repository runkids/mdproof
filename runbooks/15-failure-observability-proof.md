# Failure Observability

Validates retained failure artifacts and failed-step debug printing.

## Steps

### Step 1: Create isolated environment

```bash
ssenv create test-failure-observe
```

Expected:

- created: test-failure-observe

### Step 2: Keep failed artifacts for a failed runbook

```bash
ssenv enter test-failure-observe -- bash -c 'cd /workspace &&
bin/mdproof --isolation per-runbook --keep-failed-artifacts --report json runbooks/fixtures/failing-proof.md >/tmp/mdproof-observe-report.json 2>/tmp/mdproof-observe-debug.log || true &&
ARTIFACT_DIR=$(jq -r ".artifact_dir" /tmp/mdproof-observe-report.json) &&
ISO_DIR=$(jq -r ".isolation_dir" /tmp/mdproof-observe-report.json) &&
SCRIPT_PATH=$(jq -r ".steps[0].debug.script_path" /tmp/mdproof-observe-report.json) &&
ENV_PATH=$(jq -r ".steps[0].debug.env_path" /tmp/mdproof-observe-report.json) &&
STDOUT_PATH=$(jq -r ".steps[0].debug.stdout_path" /tmp/mdproof-observe-report.json) &&
STDERR_PATH=$(jq -r ".steps[0].debug.stderr_path" /tmp/mdproof-observe-report.json) &&
test -d "$ARTIFACT_DIR" &&
test -d "$ISO_DIR" &&
test -f "$SCRIPT_PATH" &&
test -f "$ENV_PATH" &&
test -f "$STDOUT_PATH" &&
test -f "$STDERR_PATH" &&
printf "artifact_dir=%s\nisolation_dir=%s\nscript_path=%s\nenv_path=%s\nstdout_path=%s\nstderr_path=%s\n" "$ARTIFACT_DIR" "$ISO_DIR" "$SCRIPT_PATH" "$ENV_PATH" "$STDOUT_PATH" "$STDERR_PATH"'
```

Expected:

- regex: artifact_dir=/tmp/
- regex: isolation_dir=/tmp/
- regex: script_path=/tmp/
- regex: env_path=/tmp/
- regex: stdout_path=/tmp/
- regex: stderr_path=/tmp/

### Step 3: Print failed step script and env to stderr

```bash
ssenv enter test-failure-observe -- bash -c 'cd /workspace &&
bin/mdproof --print-step-script --print-step-env --report json runbooks/fixtures/failing-proof.md >/tmp/mdproof-observe-print.json 2>/tmp/mdproof-observe-print.log || true &&
printf "json_runbook=%s\n" "$(jq -r ".runbook" /tmp/mdproof-observe-print.json)" &&
cat /tmp/mdproof-observe-print.log'
```

Expected:

- json_runbook=failing-proof.md
- failed step script
- actual output here
- failed step environment
- regex: PWD=/
- rerun with --keep-failed-artifacts

### Step 4: Cleanup

```bash
ARTIFACT_DIR=$(jq -r '.artifact_dir' /tmp/mdproof-observe-report.json)
ISO_DIR=$(jq -r '.isolation_dir' /tmp/mdproof-observe-report.json)
rm -rf "$ARTIFACT_DIR" "$ISO_DIR" \
  /tmp/mdproof-observe-report.json \
  /tmp/mdproof-observe-debug.log \
  /tmp/mdproof-observe-print.json \
  /tmp/mdproof-observe-print.log
ssrm test-failure-observe
echo cleaned
```

Expected:

- deleted: test-failure-observe
- cleaned
