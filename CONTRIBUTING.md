# Contributing

Issues and pull requests are welcome. Reports from real boards help most: please attach `report.json`
(remove anything you consider private first, such as the hostname).

## Rules

- Standard library only, Go version from `go.mod`. The binary must stay static (`CGO_ENABLED=0`).
- I/O only in `internal/probe`, judging only in `internal/checks` (pure functions), so every check is testable
  with sample data.
- A new check: add the raw fact to `probe.Facts`/`Gather`, judge it in `checks.Evaluate` with a hint that
  explains the pitfall *and* the fix, add sample data to `testdata/mkfakes.py` (`pi5-good` must stay PASS,
  `pi4-tired` shows the problem), regenerate with `python3 testdata/mkfakes.py`, extend the tests.
- JSON report fields are only added, never renamed; a breaking change raises `model.SchemaVersion`.
- Limits are fields of `checks.Expect` and get a flag.
- Every change: `./test.sh` passes, an entry at the top of `CHANGELOG.md`. Maintainers raise `VERSION`
  to release.
