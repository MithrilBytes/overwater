#!/usr/bin/env bash
# Black box smoke and regression suite: builds the binary and exercises
# every command against expected exit codes and output, including byte
# for byte golden comparisons through the real binary. Run from the repo
# root; exits nonzero if anything failed.
set -u

cd "$(dirname "$0")/.."
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export OVERWATER_CACHE_DIR="$work/cache"
bin="$work/overwater"

pass=0
fail=0

# check <name> <expected-exit> <substring|-> <cmd...>
# Runs cmd, asserts the exit code, and when substring is not "-",
# asserts combined output contains it.
check() {
  name="$1"; want="$2"; needle="$3"; shift 3
  out="$("$@" 2>&1)"
  got=$?
  if [ "$got" != "$want" ]; then
    echo "FAIL $name: exit $got, want $want"
    echo "$out" | sed 's/^/    /' | head -6
    fail=$((fail + 1))
    return
  fi
  if [ "$needle" != "-" ] && ! printf '%s' "$out" | grep -q "$needle"; then
    echo "FAIL $name: output missing: $needle"
    echo "$out" | sed 's/^/    /' | head -6
    fail=$((fail + 1))
    return
  fi
  pass=$((pass + 1))
}

go build -o "$bin" ./cmd/overwater || { echo "FAIL build"; exit 1; }

# Router and version.
check version 0 "overwater" "$bin" version
check help 0 "Usage:" "$bin" help
check unknown-command 2 "unknown command" "$bin" firehose

# Rule documentation, from the rules' own YAML.
check explain 0 "min_retries: 3" "$bin" explain retry-amplification
check explain-tripwire 0 "Tripwire:" "$bin" explain frontier-extraction
check explain-tripwire-gate 0 "Exits on:  agreement below 97%" "$bin" explain frontier-extraction
check explain-list 0 "uncached-system-prompt" "$bin" explain
check explain-unknown 2 "no rule" "$bin" explain retry-amplifcation
check explain-unknown-lists 2 "retry-amplification" "$bin" explain retry-amplifcation

# Advisor scans.
check scan-clean 0 "Keep the models you have." "$bin" scan fixtures/clean-app
check scan-findings 0 "Call site:" "$bin" scan fixtures/ts-chat-firehose
check scan-json 0 '"rule"' "$bin" scan -json fixtures/py-extraction
check scan-json-tripwire-check 0 '"threshold": 97' "$bin" scan -json fixtures/py-extraction
check scan-summary 0 "findings" "$bin" scan -summary fixtures/node-cron-summarizer
check rule-duplicate-sites 0 "duplicate" "$bin" scan -json fixtures/py-agent-pipeline
check rule-hot-temperature 0 "Temperature above zero" "$bin" scan fixtures/py-agent-pipeline
check rule-uncapped-dimensions 0 "No dimensions parameter" "$bin" scan fixtures/py-agent-pipeline
check rule-image-detail 0 "Image detail pinned" "$bin" scan fixtures/py-agent-pipeline
check rule-retry-amplification 0 "max_retries 8" "$bin" scan fixtures/py-agent-pipeline
check fail-on-any 1 - "$bin" scan -fail-on any fixtures/ts-chat-firehose
check fail-on-none 0 - "$bin" scan -fail-on none fixtures/ts-chat-firehose
check fail-on-bogus 2 "unknown --fail-on" "$bin" scan -fail-on sometimes fixtures/clean-app
check offline-refresh 0 "skipping catalog refresh" "$bin" scan -refresh -offline fixtures/clean-app

# A file path as a root scans that file and nothing else beside it.
check scan-file-root 0 "Call site: extract.py:24" "$bin" scan fixtures/py-extraction/extract.py
check scan-file-root-fails 1 - "$bin" scan -fail-on any fixtures/py-extraction/extract.py
check scan-file-root-quiet 0 "Keep the models you have." "$bin" scan fixtures/py-extraction/requirements.txt
check scan-file-root-missing 2 "scan nope.py" "$bin" scan nope.py
check scan-file-root-models-md 2 "needs a directory root" "$bin" scan -models-md fixtures/py-extraction/extract.py

# Every fixture's MODELS.md must reproduce its golden byte for byte
# through the real binary, not just the renderer test.
for fixture in ts-chat-firehose py-extraction node-cron-summarizer rag-frontier-embeddings clean-app; do
  cp -R "fixtures/$fixture" "$work/$fixture"
  "$bin" scan -models-md "$work/$fixture" > /dev/null 2>&1
  if cmp -s "$work/$fixture/MODELS.md" "goldens/$fixture.md"; then
    pass=$((pass + 1))
  else
    echo "FAIL golden-$fixture: MODELS.md drifted from golden"
    fail=$((fail + 1))
  fi
done

# Output files.
check scan-html 0 - "$bin" scan -html "$work/report.html" fixtures/py-extraction
check scan-csv 0 - "$bin" scan -csv "$work/findings.csv" fixtures/py-extraction
check scan-sarif 0 - "$bin" scan -sarif "$work/findings.sarif" fixtures/py-extraction
for f in report.html findings.csv findings.sarif; do
  if [ -s "$work/$f" ]; then pass=$((pass + 1)); else echo "FAIL empty-$f"; fail=$((fail + 1)); fi
done
check sarif-has-rule 0 "frontier-extraction" grep "frontier-extraction" "$work/findings.sarif"

# Guard lifecycle in a temp repo.
guard="$work/guard"
mkdir -p "$guard"
cat > "$guard/classify.js" <<'JS'
const client = new (require("openai"))();

async function classifyThing(text) {
  return client.chat.completions.create({
    model: "gpt-5.1",
    temperature: 0,
    max_tokens: 60,
    response_format: { type: "json_schema" },
    messages: [{ role: "user", content: text }],
  });
}
JS
bl="$work/guard-baseline.json"
check baseline-record 0 "baselined" "$bin" scan -baseline "$bl" -update-baseline "$guard"
check baseline-pass 0 "all baselined" "$bin" scan -baseline "$bl" "$guard"
# Moving the file is not a change: the entries follow it.
mkdir -p "$guard/src"
mv "$guard/classify.js" "$guard/src/classify.js"
check baseline-move 0 "all baselined" "$bin" scan -baseline "$bl" "$guard"
mv "$guard/src/classify.js" "$guard/classify.js"
rmdir "$guard/src"
cat > "$guard/legacy.js" <<'JS'
const client = new (require("openai"))();

async function fetchLegacy(text) {
  return client.completions.create({
    model: "text-davinci-003",
    max_tokens: 100,
    prompt: text,
  });
}
JS
check baseline-new-fails 1 "new: deprecated-model" "$bin" scan -baseline "$bl" "$guard"
check baseline-missing 2 "update-baseline" "$bin" scan -baseline "$work/absent.json" "$guard"
printf 'not json' > "$work/bad.json"
check baseline-corrupt 2 "not valid JSON" "$bin" scan -baseline "$work/bad.json" "$guard"

# Per repo config: disable, then budget.
printf 'disable:\n  - deprecated-model\n  - frontier-extraction\n' > "$guard/.overwater.yaml"
check config-disable 0 "Keep the models you have." "$bin" scan -fail-on any "$guard"
printf 'budget_monthly_usd: 1\n' > "$guard/.overwater.yaml"
check config-budget 1 "exceeds budget_monthly_usd" "$bin" scan "$guard"
printf 'no_such_key: 1\n' > "$guard/.overwater.yaml"
check config-unknown-key 2 - "$bin" scan "$guard"
# A file root reads the config of the directory that holds it.
printf 'disable:\n  - deprecated-model\n' > "$guard/.overwater.yaml"
check config-file-root 0 "Keep the models you have." "$bin" scan -fail-on any "$guard/legacy.js"
rm "$guard/.overwater.yaml"

# Measured volumes: the dollars move, the wording moves with them, and
# keys that match nothing are named rather than dropped.
vol="$work/volumes.json"
printf '{"sites": {"extract.py:24": 100000}, "models": {"gpt-4o": 5}}\n' > "$vol"
check volumes-measured 0 "at measured volume" "$bin" scan -volumes "$vol" fixtures/py-extraction
check volumes-priced 0 "claude-opus-5 at ~\$783/mo" "$bin" scan -volumes "$vol" fixtures/py-extraction
check volumes-unknown-key 0 "no call site uses model gpt-4o" "$bin" scan -volumes "$vol" fixtures/py-extraction
check volumes-json 0 '"volume_source": "measured"' "$bin" scan -json -volumes "$vol" fixtures/py-extraction
check volumes-estimate-untouched 0 "at estimated volume" "$bin" scan fixtures/py-extraction
printf 'not json' > "$work/bad-volumes.json"
check volumes-malformed 2 - "$bin" scan -volumes "$work/bad-volumes.json" fixtures/py-extraction
check volumes-missing 2 - "$bin" scan -volumes "$work/absent-volumes.json" fixtures/py-extraction

# Importing a provider usage export, in both shapes, into a file the
# scan reads back.
printf 'Date,Model Name,N_Requests\n2026-07-01,claude-opus-5,60000\n2026-07-02,claude-opus-5,40000\n' > "$work/usage.csv"
check volumes-import-csv 0 'model column "Model Name"' "$bin" volumes import -o "$work/imported.json" "$work/usage.csv"
check volumes-import-sum 0 '"claude-opus-5": 100000' grep claude-opus-5 "$work/imported.json"
check volumes-import-scan 0 "claude-opus-5 at ~\$783/mo" "$bin" scan -volumes "$work/imported.json" fixtures/py-extraction
printf '[{"model":"claude-opus-5","requests":25},{"model":"nope-1","requests":1}]\n' > "$work/usage.json"
check volumes-import-json 0 'model field "model"' "$bin" volumes import -o - "$work/usage.json"
check volumes-import-unknown-model 0 "not in the catalog" "$bin" volumes import -o - "$work/usage.json"
check volumes-import-bad 2 - "$bin" volumes import -o - "$work/nothing-here.csv"
check volumes-no-subcommand 2 "volumes import" "$bin" volumes
check volumes-unknown-subcommand 2 "unknown subcommand" "$bin" volumes export

# Multi root merge.
check multi-root 0 "Call site:" "$bin" scan fixtures/py-extraction fixtures/clean-app

# Config isolation. Two identical repos, one carrying a config: whatever
# it disables or prices stays inside it, in either argument order and in
# either list order.
iso="$work/iso"
mkdir -p "$iso/svc-a" "$iso/svc-b"
for svc in svc-a svc-b; do
  cat > "$iso/$svc/legacy.js" <<'JS'
const client = new (require("openai"))();

async function fetchLegacy(text) {
  return client.completions.create({
    model: "text-davinci-003",
    max_tokens: 100,
    prompt: text,
  });
}
JS
done
printf 'disable:\n  - deprecated-model\n' > "$iso/svc-a/.overwater.yaml"
check config-no-leak-ab 1 "svc-b/legacy.js" "$bin" scan -fail-on any "$iso/svc-a" "$iso/svc-b"
check config-no-leak-ba 1 "svc-b/legacy.js" "$bin" scan -fail-on any "$iso/svc-b" "$iso/svc-a"
printf '%s\n%s\n' "$iso/svc-a" "$iso/svc-b" > "$iso/ab.txt"
printf '%s\n%s\n' "$iso/svc-b" "$iso/svc-a" > "$iso/ba.txt"
check fleet-no-leak-ab 1 "fleet: 2 repos, 1 findings" "$bin" fleet -fail-on any "$iso/ab.txt"
check fleet-no-leak-ba 1 "fleet: 2 repos, 1 findings" "$bin" fleet -fail-on any "$iso/ba.txt"

# A per repo volume cannot head a merged report at a volume the body was
# not priced at: roots that disagree all fall back to the default.
printf 'volume: 1000000\n' > "$iso/svc-a/.overwater.yaml"
check volume-header-honest 0 "estimates at 10,000 calls" "$bin" scan "$iso/svc-a" "$iso/svc-b"
check volume-body-honest 0 "text-davinci-003 at ~\$120/mo" "$bin" scan "$iso/svc-b" "$iso/svc-a"
printf 'volume: 1000000\n' > "$iso/svc-b/.overwater.yaml"
check volume-agreed-header 0 "estimates at 1,000,000 calls" "$bin" scan "$iso/svc-a" "$iso/svc-b"
check volume-agreed-body 0 "text-davinci-003 at ~\$12000/mo" "$bin" scan "$iso/svc-a" "$iso/svc-b"
rm "$iso/svc-a/.overwater.yaml" "$iso/svc-b/.overwater.yaml"

# Incremental over a path git quotes: a name with non-ASCII bytes comes
# back quoted and escaped unless the listing is NUL terminated, and a
# name that matches no file is a silent miss in the guard.
if command -v git > /dev/null 2>&1; then
  inc="$work/incremental"
  mkdir -p "$inc/src"
  cp "$guard/classify.js" "$inc/src/classify.js"
  git -C "$inc" init -q .
  git -C "$inc" config user.email smoke@example.com
  git -C "$inc" config user.name Smoke
  git -C "$inc" config commit.gpgsign false
  git -C "$inc" config core.quotePath true
  git -C "$inc" config core.precomposeunicode false
  git -C "$inc" add -A
  git -C "$inc" commit -q -m base
  incbl="$work/incremental-baseline.json"
  "$bin" scan -baseline "$incbl" -update-baseline "$inc" > /dev/null 2>&1
  cp "$guard/legacy.js" "$inc/src/café.js"
  check incremental-non-ascii 1 "new: deprecated-model at src/café.js" \
    "$bin" scan -baseline "$incbl" -incremental "$inc"
fi

# Diff over two reports.
"$bin" scan -json fixtures/clean-app > "$work/old.json" 2> /dev/null
"$bin" scan -json fixtures/py-extraction > "$work/new.json" 2> /dev/null
check diff 0 "appeared" "$bin" diff "$work/old.json" "$work/new.json"
check diff-bad-file 2 - "$bin" diff "$work/old.json" "$work/nope.json"

# Fleet.
printf '%s\n%s\n' "fixtures/clean-app" "fixtures/py-extraction" > "$work/repos.txt"
check fleet 0 "fleet:" "$bin" fleet "$work/repos.txt"
check fleet-fail-on-any 1 - "$bin" fleet -fail-on any "$work/repos.txt"

# Eval generation.
check eval 0 "wrote" "$bin" eval -o "$work/evals" fixtures/py-extraction
check eval-tripwire-gate 0 "TRIPWIRE_THRESHOLD = 97" grep -r TRIPWIRE_THRESHOLD "$work/evals"
check eval-draft 0 "drafted" "$bin" eval -o "$work/evals2" -draft-prompts fixtures/py-extraction
check eval-clean 0 "Nothing to eval." "$bin" eval -o "$work/evals3" fixtures/clean-app

# The perf gate, against a stub that sleeps a fixed time per repo size
# instead of scanning: passes when time tracks input size, fails when
# the larger repo costs disproportionately more.
cat > "$work/stub-scan" <<'STUB'
#!/usr/bin/env bash
for root; do :; done
if [ "$(cat "$root"/src/* | wc -c)" -gt 200000 ]; then sleep "$STUB_LARGE"; else sleep "$STUB_SMALL"; fi
STUB
chmod +x "$work/stub-scan"
export OVERWATER_BIN="$work/stub-scan" STUB_SMALL=0.025 STUB_LARGE=0.15
check perf-gate-linear 0 "8x the input cost" bash scripts/perf-gate.sh
export STUB_LARGE=1.2
check perf-gate-superlinear 1 "growing faster than the input" bash scripts/perf-gate.sh
unset OVERWATER_BIN STUB_SMALL STUB_LARGE

# Catalog.
check catalog-show 0 "claude-haiku-4-5" "$bin" catalog show
check catalog-refresh-offline 2 "network operation" "$bin" catalog refresh -offline
# The committed snapshots under catalog/history, read back three ways.
check catalog-history 0 "PRICES MOVED" "$bin" catalog history
check catalog-history-model 0 "claude-opus-5 across" "$bin" catalog history -model claude-opus-5
check catalog-history-bad-date 2 "no snapshot dated" "$bin" catalog history -on 1999-01-01
# Built into the work directory: writing over the tracked catalog.json
# would make this check destroy uncommitted work when it fails.
cp -R catalog "$work/catalog-src"
"$bin" catalog build -dir "$work/catalog-src" > /dev/null 2>&1
if cmp -s "$work/catalog-src/catalog.json" catalog/catalog.json; then pass=$((pass + 1)); else
  echo "FAIL catalog-build-idempotent: catalog.json does not match its YAML entries"
  fail=$((fail + 1))
fi

# Packaging manifests. sync-manifests ships with the source, not with
# the release binaries, so it is exercised through its own binary.
# Whether the manifests in the tree describe the pinned release is
# internal/packaging's job.
sync="$work/sync-manifests"
go build -o "$sync" ./tools/sync-manifests || { echo "FAIL build sync-manifests"; exit 1; }
pkgdir="$work/packaging"
mkdir -p "$pkgdir"
cp flake.nix "$pkgdir/flake.nix"
cp action.yml "$pkgdir/action.yml"
fixture="internal/packaging/testdata/SHA256SUMS"
check sync-usage 2 "Usage:" "$sync"
check sync-bad-version 2 "not vX.Y.Z" "$sync" -version latest -sums "$fixture" -dir "$pkgdir"

# A release missing a platform stops the run: a manifest with an empty
# checksum installs whatever the URL happens to serve.
grep -v overwater_linux_arm64 "$fixture" > "$work/partial-sums"
check sync-missing-platform 2 "overwater_linux_arm64" \
  "$sync" -version v9.9.9 -sums "$work/partial-sums" -dir "$pkgdir"
if [ -e "$pkgdir/Formula/overwater.rb" ]; then
  echo "FAIL sync-missing-platform-wrote-anyway: a rejected release left manifests behind"
  fail=$((fail + 1))
else
  pass=$((pass + 1))
fi

check sync-writes 0 "updated 7 files" "$sync" -version v9.9.9 -sums "$fixture" -dir "$pkgdir"
check sync-idempotent 0 "already describe" "$sync" -version v9.9.9 -sums "$fixture" -dir "$pkgdir"
check sync-winget-uppercases-hash 0 "EEEEEEEE" \
  grep InstallerSha256 "$pkgdir/winget/MithrilBytes.Overwater.installer.yaml"
check sync-formula-pins-darwin-arm64 0 'sha256 "bbbbbbbb' \
  grep -A1 overwater_darwin_arm64 "$pkgdir/Formula/overwater.rb"
check sync-bumps-flake 0 'version = "9.9.9";' grep version "$pkgdir/flake.nix"

# The flake, on machines that have nix. CI runs the same evaluation.
if command -v nix > /dev/null 2>&1; then
  check flake-eval 0 - nix --extra-experimental-features "nix-command flakes" \
    flake check --no-build --all-systems
fi

# The Action is pinned by the same run that writes the manifests, so the
# two can never disagree about which release is current. A half pinned
# Action fails only on the platform nobody tested.
mkdir -p "$work/fresh-pkg"
cp flake.nix action.yml "$work/fresh-pkg/"
check sync-pins-action 0 "action.yml" \
  "$sync" -version v9.9.9 -sums "$fixture" -dir "$work/fresh-pkg"
if grep -q 'version="v9.9.9"' "$work/fresh-pkg/action.yml"; then
  pass=$((pass + 1))
else
  echo "FAIL sync-pins-action-version: action.yml still names the old release"
  fail=$((fail + 1))
fi

echo
echo "smoke: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
