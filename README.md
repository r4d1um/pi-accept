# pi-accept

[![ci](https://github.com/r4d1um/pi-accept/actions/workflows/ci.yml/badge.svg)](https://github.com/r4d1um/pi-accept/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/r4d1um/pi-accept)](https://github.com/r4d1um/pi-accept/releases/latest)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**An acceptance test for Raspberry Pis.** One static binary, no dependencies, no install. It checks a Pi
end to end (power, thermals, firmware, storage, kernel log, network, security), puts it under load, and
explains every problem it finds: what it means and how to fix it.

Use it on a Pi you just bought (new or used), after flashing a new SD card, or when a Pi behaves oddly.

<p align="center"><img src="docs/pi5-good.png" alt="piaccept on a healthy Pi 5 (sample data): all checks pass" width="620"></p>

<sub>Sample data (`--fake testdata/fakes/pi5-good`). A worn-out Pi 4 with under-voltage, SD errors and an old OS
looks like [this](docs/pi4-tired.png): every problem comes with an explanation and a fix.</sub>

```
exit 0 = PASS · 1 = WARN · 2 = FAIL
report: /var/lib/piaccept/report.json and report.md (UTC)
```

## Install

Pick the binary for your OS (`dpkg --print-architecture`) from the
[latest release](https://github.com/r4d1um/pi-accept/releases/latest):

| Binary | `dpkg` arch | Boards |
|---|---|---|
| `piaccept-linux-arm64` | `arm64` | Pi 3/3+/4/400/5, CM4/CM5, Zero 2 W on a 64-bit OS |
| `piaccept-linux-armv7` | `armhf` | Pi 2/3/4 on a 32-bit OS |
| `piaccept-linux-armv6` | `armhf` on ARMv6 | Pi 1, Zero, Zero W |

```sh
arch=arm64   # or armv7 / armv6
curl -fsSLO https://github.com/r4d1um/pi-accept/releases/latest/download/piaccept-linux-$arch
curl -fsSLO https://github.com/r4d1um/pi-accept/releases/latest/download/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS
chmod +x piaccept-linux-$arch && sudo mv piaccept-linux-$arch /usr/local/bin/piaccept
```

Or build from source with Go ≥ 1.22: `go install github.com/r4d1um/pi-accept/cmd/piaccept@latest`

## Before you run it

> **Use at your own risk.** pi-accept comes without any warranty ([MIT licence](LICENSE)). By default it runs
> two active tests:
> - **CPU stress** (`--stress`, default 60 s): all cores at full load. The Pi gets hot and draws its maximum
>   current; a weak power supply or poor cooling can make it throttle or crash. That is what the test is for,
>   but run it when a crash does no harm.
> - **Storage test** (`--storage`, default 256 MB, in `--io-dir`): writes a file and reads it back. This wears
>   flash memory a little and can hit a failing or fake SD card hard.
>
> **Back up important data first.** `piaccept --quick` runs only the read-only checks.

## Usage

```sh
sudo piaccept                                # static checks + 60 s all-core stress + 256 MB storage test
sudo piaccept --stress 10m --storage 1024    # real burn-in
piaccept --quick                             # static checks only, no root needed (some checks skip)
piaccept --version
```

All limits are flags (`--temp-warn`, `--temp-fail`, `--stress-max`, `--min-write`, `--min-read`,
`--disk-warn`, `--disk-fail`, `--ping-warn`, …), see `piaccept --help`. Colour turns off with `NO_COLOR`
or when the output is not a terminal; `--color` forces it. `--dir ""` writes no report.

## What it checks

- **Board**: model, revision code decoded (RAM, SoC), OTP *warranty bit* (over-volted in the past), visible RAM vs. label
- **Power & thermals**: `get_throttled` bits since boot (under-voltage!), idle temperature, core voltage, clock
- **Firmware & OS**: end-of-life Debian releases, 64-bit kernel with 32-bit userland, bootloader EEPROM updates,
  overclocking, edits in the ignored `/boot/config.txt` on Bookworm, enabled overlays
- **Storage**: read-only remount, root never expanded, usage, `noatime`, SD manufacturer ID (counterfeit hint), swap on SD
- **Kernel log**: under-voltage, mmc timeouts, I/O and EXT4 errors, OOM kills, USB over-current, broken UAS adapters
- **System**: NTP sync (a Pi has no RTC before the Pi 5), failed units, hardware watchdog
- **Security**: default hostname, default `pi` user, SSH root or password login
- **Network**: default route, gateway ping and loss, Wi-Fi power saving, Wi-Fi country, rfkill
- **Stress** (active): all cores busy, live temperature sparkline, throttling and clock drops under load
- **Storage test** (active): fsync'd write, page-cache drop, read-back SHA-256 (catches fake-capacity cards)

Tip: under-voltage is the root cause of most SD card, mmc and EXT4 errors. If both show up, fix the power
supply first.

## Report

`report.json` is meant for scripts and fleets: every result has an `id`, a `status` (`pass`/`warn`/`fail`/`skip`),
the measured value and a `hint`. Fields are only ever added, never renamed; a breaking change raises `schema`.

## Try it without a Pi

The repo ships two sample machines (`testdata/fakes/`): a healthy Pi 5 and a tired Pi 4.

```sh
go run ./cmd/piaccept --fake testdata/fakes/pi4-tired --stress 20s --interval 150ms --storage 64 --io-dir /tmp --dir ""
```

## Development

```
cmd/piaccept/      flags, main flow
internal/probe     all I/O (Real or Fake source)
internal/checks    pure logic: facts → results with hints
internal/phases    active tests (stress, storage)
internal/ui        terminal output
internal/report    JSON / Markdown
internal/model     report types (schema)
testdata/          mkfakes.py → fakes/<machine>
```

Standard library only. `tools/screenshots.sh` regenerates the pictures in `docs/` from the fake Pis.
Run `./test.sh` (gofmt, vet incl. ARMv6, unit tests, demo verdicts on the fake Pis).
See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
