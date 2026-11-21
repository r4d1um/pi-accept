#!/usr/bin/env bash
# Regenerates docs/pi5-good.png and docs/pi4-tired.png: piaccept run on the fake Pis (sample data only).
# Needs python3 with pyte, pillow, fonttools; DejaVu Sans Mono (+ Noto Color Emoji for icons).
set -euo pipefail
cd "$(dirname "$0")/.."
bin=$(mktemp -d)/piaccept
go build -ldflags "-X main.version=$(tr -d '[:space:]' < VERSION)" -o "$bin" ./cmd/piaccept
for fake in pi5-good pi4-tired; do
  python3 tools/shot.py 90 230 "docs/$fake.png" \
    "$bin" --fake "testdata/fakes/$fake" --stress 8s --interval 200ms --storage 64 --io-dir "${TMPDIR:-/tmp}" --dir ""
done
