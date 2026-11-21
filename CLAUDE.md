# Rules for Claude in this repo

pi-accept is **public** (MIT). Nothing private goes in: no real hostnames, IPs, user names, serials,
tokens, or the owner's start-up splash (it is a pure command-line tool, no splash). Sample data in
`testdata/` uses made-up values (user `ops`, 192.168.1.x).

Tools: only Go (version from `go.mod`), standard library only, so no `tools/session-setup.sh` is needed.

## Map
- `cmd/piaccept/` flags and main flow · `internal/probe` all I/O (Real/Fake) · `internal/checks` pure logic ·
  `internal/phases` stress/storage · `internal/ui` terminal · `internal/report` JSON/MD · `internal/model` schema
- `tools/screenshots.sh` renders `docs/*.png` from the fakes (needs pyte, pillow, fonttools)
- `testdata/mkfakes.py` generates `testdata/fakes/{pi5-good,pi4-tired}` (commit both)
- `.github/workflows/ci.yml` tests, cross-builds, and releases when `VERSION` names a new tag

## Every change
`./test.sh` passes (plus `go test -race ./... && GOARCH=386 go vet ./...`), CHANGELOG entry at the top,
`VERSION` up when it should be released. JSON fields only added, never renamed. After UI changes show demo
screenshots of both fakes in the chat.
