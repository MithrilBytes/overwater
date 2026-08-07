#!/usr/bin/env bash
# Scaling gate for the scanner. Times two synthetic repos of identical
# shape, one size_step times the bytes of the other, and fails when the
# larger one costs disproportionately more time. Run from anywhere;
# OVERWATER_BIN names a prebuilt binary, otherwise one is built.
#
# A ratio, not a wall clock floor, because CI runners differ in speed by
# several times and the same absolute number cannot mean the same thing
# on all of them. Dividing two measurements taken back to back on one
# runner cancels the machine out, so the bound below means the same
# thing everywhere and can be set close enough to catch something.
#
# Wall clock over the shipped binary, not go test -bench: the benchmark
# harness reports ns/op that only mean something next to a stored
# absolute number, which is the runner variance problem again, and it
# times Analyze alone. The binary covers the walk, the rules and the
# renderer too, so a quadratic anywhere on that path trips this.
#
# What this does not catch: a slowdown that hits both sizes equally.
# The ratio divides a constant factor out along with the machine. The
# analyze benchmark step prints files/s for that.
set -u
export LC_ALL=C

cd "$(dirname "$0")/.."

# Eight times the input should cost about eight times the time; the
# measured ratio is 6.5 to 7.7, under 8 because a fixed startup cost
# sits inside both measurements. 20 is over twice that, well out of
# reach of runner noise, and comfortably under both the 64 a fully
# quadratic pass gives at this step and the 26 measured from a build
# that recomputes file scoped facts per call site, which is one of the
# two bugs this defends against. The other cost 57x on one 95KB file.
max_ratio=20

# 8 files of 40 call site blocks is about 64KB and 75ms of work, long
# enough that millisecond timing and process startup are noise against
# it, short enough that ten runs of it stay inside a CI step.
files=8
small_blocks=40
size_step=8

# The small scan is the denominator: a hiccup there inflates it, which
# deflates the ratio and hides a regression rather than inventing one.
# It is also the cheap one, so it gets the extra samples.
small_reps=7
large_reps=3

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export OVERWATER_CACHE_DIR="$work/cache"

bin="${OVERWATER_BIN:-}"
if [ -z "$bin" ]; then
  bin="$work/overwater"
  go build -o "$bin" ./cmd/overwater || exit 1
fi

# gen <dir> <blocks> writes files identical sources of blocks call
# sites each, so bytes and call sites scale together.
gen() {
  local dir="$1" blocks="$2" body="" i
  for ((i = 0; i < blocks; i++)); do
    body+="
async function handler$i(text) {
  return client.chat.completions.create({
    model: \"gpt-5-mini\",
    temperature: 0,
    max_tokens: 200,
    messages: [{ role: \"user\", content: text }],
  });
}
"
  done
  mkdir -p "$dir/src"
  for ((i = 0; i < files; i++)); do
    printf '%s\n%s' 'const client = new (require("openai"))();' "$body" > "$dir/src/h$i.js"
  done
}

# scan_ms <dir> <reps> prints the fastest scan in milliseconds. The
# fastest, not the mean: noise on a shared runner only ever adds time,
# so the floor is the stable statistic and the one that keeps a slow
# neighbouring job from failing the build.
scan_ms() {
  local dir="$1" reps="$2" best=0 i secs ms
  for ((i = 0; i < reps; i++)); do
    TIMEFORMAT='%3R'
    secs=$( { time "$bin" scan -summary "$dir" > /dev/null 2>&1 ; } 2>&1 )
    ms=$((10#${secs%.*} * 1000 + 10#${secs#*.}))
    if [ "$i" -eq 0 ] || [ "$ms" -lt "$best" ]; then
      best=$ms
    fi
  done
  printf '%s' "$best"
}

gen "$work/small" "$small_blocks"
gen "$work/large" $((small_blocks * size_step))
"$bin" scan -summary "$work/small" > /dev/null 2>&1 # warm the page cache

small_ms="$(scan_ms "$work/small" "$small_reps")"
large_ms="$(scan_ms "$work/large" "$large_reps")"
small_kb=$(($(cat "$work/small"/src/* | wc -c) / 1024))
large_kb=$(($(cat "$work/large"/src/* | wc -c) / 1024))

if [ "$small_ms" -lt 20 ]; then
  echo "perf-gate: the ${small_kb}KB scan measured ${small_ms}ms, too short to divide by; raise small_blocks" >&2
  exit 1
fi

tenths=$((large_ms * 10 / small_ms))
ratio="$((tenths / 10)).$((tenths % 10))"
echo "perf-gate: ${small_kb}KB in ${small_ms}ms, ${large_kb}KB in ${large_ms}ms, ${size_step}x the input cost ${ratio}x the time (bound ${max_ratio}x)"

if [ "$tenths" -gt $((max_ratio * 10)) ]; then
  echo "perf-gate: FAIL. Analysis time is growing faster than the input." >&2
  echo "  Look for work that is done per call site but reads the whole file" >&2
  echo "  or the whole repo: a region that grows with the file, a file scoped" >&2
  echo "  regex run per site, a pass over every site inside a loop over sites." >&2
  echo "  internal/scan/perf_test.go pins the two that got us before." >&2
  exit 1
fi
