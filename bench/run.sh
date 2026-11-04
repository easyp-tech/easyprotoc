#!/usr/bin/env bash
# Benchmarks easyprotoc against protoc and protocompile (the compiler inside
# buf) on pinned googleapis corpora, or checks that easyprotoc writes the same
# descriptors as protoc. Needs Go, git and network on the first run; tools and
# corpora are cached in bench/.cache.
#
#   bench/run.sh            time every implementation on every corpus
#   bench/run.sh parity     compare easyprotoc output with protoc, file by file
#
# Env:
#   RUNS, WARMUP      measured and warmup runs per command (default 10, 2)
#   CORPORA           default "wkt compute api-common googleapis"
#   IMPLS             default "protoc protocompile easyprotoc [$CANDIDATE_NAME]"
#   CANDIDATE         any other protoc-compatible binary to add to the table,
#                     named CANDIDATE_NAME (default "candidate")
#   PROTOC            use this protoc instead of downloading the pinned release;
#                     its include/ is taken from PROTOC_INCLUDE or ../include
#   GOOGLEAPIS        use this googleapis checkout instead of the pinned one
#   OUT               results file (default bench/results/<date>-<os>-<arch>.jsonl)
# Kept bash-3.2 compatible (macOS /bin/bash).
set -euo pipefail

BENCH="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(dirname "$BENCH")"
CACHE="$BENCH/.cache"
BIN="$CACHE/bin"

PROTOC_VERSION=36.2
GOOGLEAPIS_REPO=https://github.com/googleapis/googleapis.git
GOOGLEAPIS_COMMIT=8f11fef85d174acbf58caac5afd6456e982c3595

mode="${1:-bench}"
case "$mode" in
  bench | parity) ;;
  *)
    echo "usage: bench/run.sh [bench|parity]" >&2
    exit 2
    ;;
esac

mkdir -p "$BIN"
echo "build easyprotoc and bench tools" >&2
(cd "$ROOT" && go build -o "$BIN/easyprotoc" ./cmd/easyprotoc)
(cd "$BENCH" && go build -o "$BIN/" ./cmd/benchrun ./cmd/fdsdiff ./cmd/fetch-protoc ./cmd/protocompile-cli)

if [ -z "${PROTOC:-}" ]; then
  PROTOC="$CACHE/protoc-$PROTOC_VERSION/bin/protoc"
  if [ ! -x "$PROTOC" ]; then
    "$BIN/fetch-protoc" -version "$PROTOC_VERSION" -checksums "$BENCH/checksums.txt" \
      -dir "$CACHE/protoc-$PROTOC_VERSION" >/dev/null
  fi
fi
PROTOC_INCLUDE="${PROTOC_INCLUDE:-$(dirname "$(dirname "$PROTOC")")/include}"

if [ -z "${GOOGLEAPIS:-}" ]; then
  GOOGLEAPIS="$CACHE/googleapis"
  if [ "$(git -C "$GOOGLEAPIS" rev-parse HEAD 2>/dev/null || true)" != "$GOOGLEAPIS_COMMIT" ]; then
    echo "fetch googleapis $GOOGLEAPIS_COMMIT" >&2
    rm -rf "$GOOGLEAPIS"
    git init -q "$GOOGLEAPIS"
    git -C "$GOOGLEAPIS" fetch -q --depth 1 "$GOOGLEAPIS_REPO" "$GOOGLEAPIS_COMMIT"
    git -C "$GOOGLEAPIS" -c advice.detachedHead=false checkout -q FETCH_HEAD
  fi
fi

# Corpus lists, one proto path per line relative to googleapis:
#   wkt         google/protobuf/descriptor.proto: process startup
#   compute     google/cloud/compute/v1/compute.proto: one 4 MB file
#   api-common  google/{api,rpc,type,longrunning}: a small real project
#   googleapis  google/** and grafeas/**, about 7.4k files
# The top-level preview/ is left out: it redefines packages from google/.
CORPUS_DIR="$CACHE/corpus"
mkdir -p "$CORPUS_DIR"
list() { (cd "$GOOGLEAPIS" && find "$@" -name '*.proto' | LC_ALL=C sort); }
echo "google/protobuf/descriptor.proto" >"$CORPUS_DIR/wkt.args"
echo "google/cloud/compute/v1/compute.proto" >"$CORPUS_DIR/compute.args"
list google/api google/rpc google/type google/longrunning >"$CORPUS_DIR/api-common.args"
list google grafeas >"$CORPUS_DIR/googleapis.args"

CANDIDATE_NAME="${CANDIDATE_NAME:-candidate}"
impl_cmd() {
  case "$1" in
    protoc) echo "$PROTOC" ;;
    protocompile) echo "$BIN/protocompile-cli" ;;
    easyprotoc) echo "$BIN/easyprotoc" ;;
    "$CANDIDATE_NAME") echo "${CANDIDATE:?CANDIDATE is not set}" ;;
    *)
      echo "unknown implementation: $1" >&2
      return 1
      ;;
  esac
}

# The flags buf and easyp ship descriptors with.
compile_args() {
  echo "-I$GOOGLEAPIS -I$PROTOC_INCLUDE --include_imports --include_source_info --descriptor_set_out=$2 @$CORPUS_DIR/$1.args"
}

corpora="${CORPORA:-wkt compute api-common googleapis}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [ "$mode" = parity ]; then
  status=0
  for corpus in $corpora; do
    for impl in protoc easyprotoc; do
      # Both print unused-import warnings on googleapis; show output only on failure.
      # shellcheck disable=SC2046
      if ! $(impl_cmd "$impl") $(compile_args "$corpus" "$tmp/$impl.pb") >"$tmp/log" 2>&1; then
        cat "$tmp/log" >&2
        echo "$corpus: $impl failed" >&2
        exit 1
      fi
    done
    if "$BIN/fdsdiff" "$tmp/protoc.pb" "$tmp/easyprotoc.pb" >"$tmp/log" 2>&1; then
      echo "$corpus: same as protoc"
    else
      cat "$tmp/log"
      echo "$corpus: DIFFERENT from protoc"
      status=1
    fi
  done
  exit "$status"
fi

impls="${IMPLS:-protoc protocompile easyprotoc}"
if [ -n "${CANDIDATE:-}" ] && [ -z "${IMPLS:-}" ]; then
  impls="$impls $CANDIDATE_NAME"
fi
baseline=protoc
case " $impls " in
  *" protoc "*) ;;
  *) baseline="${impls%% *}" ;;
esac
mkdir -p "$BENCH/results"
out="${OUT:-$BENCH/results/$(date +%Y%m%d)-$(uname -s | tr 'A-Z' 'a-z')-$(uname -m).jsonl}"
versions="$("$PROTOC" --version 2>/dev/null || echo "protoc $PROTOC_VERSION (not run)"), protocompile $(cd "$BENCH" && go list -m -f '{{.Version}}' github.com/bufbuild/protocompile), easyprotoc $(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown), googleapis $(git -C "$GOOGLEAPIS" rev-parse --short HEAD 2>/dev/null || echo unknown)"
for corpus in $corpora; do
  for impl in $impls; do
    # shellcheck disable=SC2046
    "$BIN/benchrun" run -name "$impl" -corpus "$corpus" \
      -n "${RUNS:-10}" -warmup "${WARMUP:-2}" -out "$out" -- \
      $(impl_cmd "$impl") $(compile_args "$corpus" "$tmp/$impl.pb")
  done
done
{
  echo "$versions"
  "$BIN/benchrun" report -baseline "$baseline" "$out"
} | tee "${out%.jsonl}.md"
