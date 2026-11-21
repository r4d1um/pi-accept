#!/usr/bin/env bash
# Fancy test runner for pi-accept: vet, unit tests, demo verdicts on the fake Pis.
set -uo pipefail
cd "$(dirname "$0")"
c() { printf '\033[38;2;%sm%s\033[0m' "$1" "$2"; }
ok=0; bad=0
step() {
  local name=$1; shift
  printf '  %s %-38s' "$(c '120;90;255' '◆')" "$name"
  local t0=$SECONDS out
  if out=$("$@" 2>&1); then
    printf '%s %s\n' "$(c '80;220;120' '✔ PASS')" "$(c '120;125;140' "$((SECONDS - t0))s")"; ok=$((ok + 1))
  else
    printf '%s\n' "$(c '255;85;95' '✖ FAIL')"; sed 's/^/      │ /' <<<"$out" | tail -20; bad=$((bad + 1))
  fi
}
demo() { # fake expected-exit
  local rc; "$BIN" --fake "testdata/fakes/$1" --stress 2s --interval 10ms --storage 8 --io-dir "${TMPDIR:-/tmp}" --dir "" >/dev/null; rc=$?
  [ "$rc" = "$2" ] || { echo "exit $rc, want $2"; return 1; }
}
BIN=$(mktemp -d)/piaccept; go build -o "$BIN" ./cmd/piaccept || exit 1
printf '\n  %s\n\n' "$(c '255;60;120' '🍓 piaccept test suite')"
step "gofmt"              bash -c '[ -z "$(gofmt -l .)" ] || { gofmt -l .; exit 1; }'
step "go vet"             go vet ./...
step "go vet (arm v6)"     env GOARCH=arm GOARM=6 go vet ./...
step "unit tests"         go test ./...
step "demo pi5-good → PASS (0)"  demo pi5-good 0
step "demo pi4-tired → FAIL (2)" demo pi4-tired 2
printf '\n  %s  %s\n\n' "$(c '80;220;120' "✔ $ok passed")" "$(c '255;85;95' "✖ $bad failed")"
[ "$bad" = 0 ]
