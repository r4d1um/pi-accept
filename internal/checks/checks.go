// Package checks turns probe.Facts into results. Pure: no I/O, no clock.
package checks

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/r4d1um/pi-accept/internal/model"
	"github.com/r4d1um/pi-accept/internal/probe"
)

// Expect holds every limit; all of them are flags in cmd/piaccept.
type Expect struct {
	TempWarnC   float64
	TempFailC   float64
	DiskWarnPct float64
	DiskFailPct float64
	PingWarnMs  float64
	MinWriteMBs float64
	MinReadMBs  float64
	StressMaxC  float64
	CoreVoltMin float64
}

func DefaultExpect() Expect {
	return Expect{TempWarnC: 65, TempFailC: 80, DiskWarnPct: 80, DiskFailPct: 95,
		PingWarnMs: 20, MinWriteMBs: 10, MinReadMBs: 20, StressMaxC: 82}
}

type builder struct {
	group string
	out   []model.Result
}

func (b *builder) add(id, title string, st model.Status, val, hint string) {
	b.out = append(b.out, model.Result{ID: id, Group: b.group, Title: title, Status: st, Value: val, Hint: hint})
}

// Evaluate runs all static checks.
func Evaluate(f probe.Facts, e Expect) []model.Result {
	b := &builder{}
	board, haveBoard := ParseBoard(f.CPUInfo)

	// ── Board ────────────────────────────────────────────────────────────
	b.group = "Board"
	if f.Model == "" {
		b.add("board.model", "Raspberry Pi detected", model.Fail, "no /proc/device-tree/model",
			"This does not look like a Raspberry Pi (or /proc/device-tree is missing).")
	} else {
		b.add("board.model", "Model", model.Pass, f.Model, "")
	}
	if haveBoard {
		b.add("board.rev", "Revision code", model.Info,
			fmt.Sprintf("%06x · %s · %s · %d MB", board.Raw, board.TypeName, board.Processor, board.MemMB), "")
		if board.Warranty {
			b.add("board.warranty", "Warranty bit", model.Warn, "OTP warranty bit SET",
				"force_turbo=1 with over_voltage was used on this board at some point. It is burned in OTP and permanent — a used board sold as 'new' is a red flag.")
		} else {
			b.add("board.warranty", "Warranty bit", model.Pass, "not set", "")
		}
		if tot := memKB(f.MemInfo, "MemTotal"); tot > 0 && board.MemMB > 0 {
			got := tot / 1024
			// Firmware/GPU/CMA reserve memory, so MemTotal is lower than the label.
			if float64(got) < float64(board.MemMB)*0.80 {
				b.add("board.ram", "RAM vs. revision code", model.Warn, fmt.Sprintf("%d MB visible of %d MB", got, board.MemMB),
					"A lot of RAM is reserved. Check gpu_mem / cma in config.txt, or a 32-bit kernel without LPAE on an 8 GB board.")
			} else {
				b.add("board.ram", "RAM vs. revision code", model.Pass, fmt.Sprintf("%d MB visible of %d MB", got, board.MemMB), "")
			}
		}
	}
	if sn := field(f.CPUInfo, "Serial"); sn != "" {
		b.add("board.serial", "Serial", model.Info, sn, "")
	}

	// ── Power & thermals ────────────────────────────────────────────────
	b.group = "Power & thermals"
	if n, ok := Throttled(f.Throttled); !ok {
		b.add("power.throttled", "Throttling flags", model.Skip, "vcgencmd missing",
			"Install libraspberrypi-bin / raspi-utils to get vcgencmd; without it under-voltage is invisible.")
	} else if n == 0 {
		b.add("power.throttled", "Throttling flags since boot", model.Pass, "0x0 — clean", "")
	} else {
		st := model.Warn
		if n&0x1 != 0 || n&(1<<16) != 0 {
			st = model.Fail
		}
		hint := "Throttling happened since boot."
		if n&((1<<0)|(1<<16)) != 0 {
			hint = fmt.Sprintf("UNDER-VOLTAGE: the #1 cause of SD corruption and random crashes. Use a proper %s supply and a short, thick cable; no USB hubs powered from the Pi.", board.PSU())
		} else if n&((1<<3)|(1<<19)) != 0 {
			hint = "Thermal limit hit. Add a heatsink/fan (Pi 5: active cooler) and check the case airflow."
		}
		b.add("power.throttled", "Throttling flags since boot", st,
			fmt.Sprintf("0x%x — %s", n, strings.Join(ThrottleText(n), ", ")), hint)
	}
	if t, ok := Number(f.Temp); ok {
		st, hint := model.Pass, ""
		switch {
		case t >= e.TempFailC:
			st, hint = model.Fail, "Idle temperature is far too high: cooling is missing or the board is under constant load."
		case t >= e.TempWarnC:
			st, hint = model.Warn, "Warm at idle. Soft throttling starts at 80 °C (Pi 5: fan curve), hard at 85 °C."
		}
		b.add("power.temp", "SoC temperature (idle)", st, fmt.Sprintf("%.1f °C", t), hint)
	}
	if v, ok := Number(f.Volts); ok {
		// Pi 5 idles around 0.72 V, older boards ~0.85–1.2 V: informational only.
		b.add("power.volts", "Core voltage", model.Info, fmt.Sprintf("%.4f V", v), "")
	}
	if hz, ok := Number(f.ArmClock); ok {
		b.add("power.clock", "ARM clock (now)", model.Info, fmt.Sprintf("%.0f MHz", hz/1e6), "")
	}

	// ── Firmware & OS ───────────────────────────────────────────────────
	b.group = "Firmware & OS"
	osr := kv(f.OSRelease)
	if name := osr["PRETTY_NAME"]; name != "" {
		st, hint := model.Pass, ""
		switch osr["VERSION_CODENAME"] {
		case "stretch", "jessie", "wheezy", "buster":
			st, hint = model.Fail, "This Debian release is end-of-life: no security updates. Reflash with a current Raspberry Pi OS."
		case "bullseye":
			st, hint = model.Warn, "Bullseye is 'oldoldstable' territory now — plan the move to a current release (config moves to /boot/firmware, NetworkManager)."
		}
		b.add("os.release", "OS release", st, name, hint)
	}
	if f.UnameM != "" {
		val := f.UnameM + " kernel / " + orDash(f.DpkgArch) + " userland"
		if f.UnameM == "aarch64" && f.DpkgArch == "armhf" {
			b.add("os.arch", "Kernel vs. userland", model.Warn, val,
				"64-bit kernel with 32-bit userland: the default on 32-bit images for Pi 4/5. Docker pulls arm/v7 images, Go/Node binaries may pick the wrong arch. Set arm_64bit=0 or use the 64-bit image.")
		} else {
			b.add("os.arch", "Kernel vs. userland", model.Pass, val, "")
		}
	}
	if f.EEPROM != "" {
		switch {
		case strings.Contains(f.EEPROM, "UPDATE AVAILABLE"):
			b.add("fw.eeprom", "Bootloader EEPROM", model.Warn, "update available",
				"Run `sudo rpi-eeprom-update -a` and reboot: newer bootloaders fix USB/NVMe boot and power issues.")
		case strings.Contains(f.EEPROM, "up to date") || strings.Contains(f.EEPROM, "up-to-date"):
			b.add("fw.eeprom", "Bootloader EEPROM", model.Pass, "up to date", "")
		default:
			b.add("fw.eeprom", "Bootloader EEPROM", model.Info, firstLine(f.EEPROM), "")
		}
	} else if board.Family() == "pi4" || board.Family() == "pi5" {
		b.add("fw.eeprom", "Bootloader EEPROM", model.Skip, "rpi-eeprom-update missing", "Install rpi-eeprom to keep the Pi 4/5 bootloader updated.")
	}
	if f.ConfigTxt != "" {
		b.add("fw.config", "Boot config in use", model.Info, f.ConfigPath, "")
		if f.ConfigPath == "/boot/firmware/config.txt" && meaningfulLines(f.LegacyConfigTxt) > 0 {
			b.add("fw.legacycfg", "Edits in old /boot/config.txt", model.Warn,
				fmt.Sprintf("%d active lines", meaningfulLines(f.LegacyConfigTxt)),
				"Since Bookworm the firmware reads /boot/firmware/config.txt. Settings in /boot/config.txt are IGNORED — classic pitfall from old tutorials.")
		}
		var oc []string
		for _, k := range []string{"over_voltage", "over_voltage_delta", "arm_freq", "force_turbo", "gpu_freq"} {
			if v, ok := configValue(f.ConfigTxt, k); ok {
				oc = append(oc, k+"="+v)
			}
		}
		if len(oc) > 0 {
			b.add("fw.overclock", "Overclocking", model.Warn, strings.Join(oc, " "),
				"Overclocked. Acceptance results only count for this config; run the stress phase and watch for throttling and SD errors.")
		} else {
			b.add("fw.overclock", "Overclocking", model.Pass, "stock clocks", "")
		}
		if v, ok := configValue(f.ConfigTxt, "gpu_mem"); ok {
			b.add("fw.gpumem", "gpu_mem", model.Info, v+" MB", "")
		}
		ifaces := []string{}
		for _, l := range append(configLines(f.ConfigTxt, "dtparam="), configLines(f.ConfigTxt, "dtoverlay=")...) {
			ifaces = append(ifaces, strings.SplitN(l, "=", 2)[1])
		}
		if v, ok := configValue(f.ConfigTxt, "enable_uart"); ok && v == "1" {
			ifaces = append(ifaces, "uart")
		}
		if len(ifaces) > 0 {
			b.add("fw.ifaces", "Enabled interfaces/overlays", model.Info, strings.Join(ifaces, ", "), "")
		}
	}

	// ── Storage ─────────────────────────────────────────────────────────
	b.group = "Storage"
	if fs := strings.Fields(f.FindmntRoot); len(fs) >= 3 {
		src, typ, opts := fs[0], fs[1], fs[2]
		if strings.HasPrefix(opts, "ro,") || opts == "ro" {
			b.add("disk.ro", "Root filesystem writable", model.Fail, src+" mounted read-only",
				"The kernel remounted / read-only — almost always SD card errors or power loss. Check the kernel log below and replace the card.")
		} else {
			b.add("disk.ro", "Root filesystem", model.Pass, src+" ("+typ+", rw)", "")
		}
		if strings.Contains(opts, "noatime") {
			b.add("disk.noatime", "noatime", model.Pass, "set", "")
		} else {
			b.add("disk.noatime", "noatime", model.Info, "not set", "Adding noatime in /etc/fstab saves SD writes.")
		}
		b.out = append(b.out, expandCheck(src, f.Lsblk, b.group)...)
	}
	if lines := strings.Split(f.DFRoot, "\n"); len(lines) >= 2 {
		fs := strings.Fields(lines[1])
		if len(fs) >= 5 {
			size, _ := strconv.ParseFloat(fs[1], 64)
			used, _ := strconv.ParseFloat(fs[2], 64)
			if size > 0 {
				pct := used / size * 100
				st := model.Pass
				hint := ""
				if pct >= e.DiskFailPct {
					st, hint = model.Fail, "Root is almost full: apt, journald and Docker will fail in odd ways."
				} else if pct >= e.DiskWarnPct {
					st, hint = model.Warn, "Root is filling up. Check /var/log, /var/lib/docker and apt caches."
				}
				b.add("disk.usage", "Root usage", st, fmt.Sprintf("%.0f %% of %s", pct, human(uint64(size))), hint)
			}
		}
	}
	if f.SDManfid != "" {
		vendor, known := sdVendors[strings.ToLower(f.SDManfid)]
		val := fmt.Sprintf("%s · %s · made %s", orDash(vendor), orDash(f.SDName), orDash(f.SDDate))
		if known {
			b.add("disk.sd", "SD card", model.Pass, val, "")
		} else {
			b.add("disk.sd", "SD card", model.Warn, "manfid "+f.SDManfid+" · "+orDash(f.SDName),
				"Unknown manufacturer ID: typical for counterfeit or no-name cards. Run the storage phase (--storage) to verify real speed and data integrity.")
		}
	}
	if strings.Contains(f.Swaps, "/var/swap") || strings.Contains(f.Swaps, "/swapfile") {
		b.add("disk.swap", "Swap on SD", model.Info, "swap file on root",
			"Swap on an SD card wears it out. Consider zram (Bookworm: rpi-swap / zram-tools) or more RAM.")
	}

	// ── Kernel log ──────────────────────────────────────────────────────
	b.group = "Kernel log (this boot)"
	b.out = append(b.out, logChecks(f.KernelLog, b.group)...)

	// ── System ──────────────────────────────────────────────────────────
	b.group = "System"
	td := kv(f.Timedatectl)
	switch td["NTPSynchronized"] {
	case "yes":
		b.add("sys.ntp", "Clock synchronised", model.Pass, orDash(td["Timezone"]), "")
	case "no":
		b.add("sys.ntp", "Clock synchronised", model.Fail, "NTP not synced",
			"The Pi has no RTC (except Pi 5 with battery). Without NTP the clock starts at the last shutdown time: TLS, apt and logs break. Check systemd-timesyncd and DNS.")
	}
	if f.FailedUnits != "" {
		n := len(strings.Split(f.FailedUnits, "\n"))
		b.add("sys.failed", "Failed systemd units", model.Warn, fmt.Sprintf("%d: %s", n, unitNames(f.FailedUnits)),
			"Look at `systemctl status <unit>` / `journalctl -u <unit> -b`.")
	} else {
		b.add("sys.failed", "Failed systemd units", model.Pass, "none", "")
	}
	if f.Watchdog {
		if v := confValue(f.SystemdConf, "RuntimeWatchdogSec"); v != "" && v != "0" && v != "off" {
			b.add("sys.watchdog", "Hardware watchdog", model.Pass, "armed ("+v+")", "")
		} else {
			b.add("sys.watchdog", "Hardware watchdog", model.Info, "available, not armed",
				"Set RuntimeWatchdogSec=15 in /etc/systemd/system.conf so a hung Pi reboots by itself.")
		}
	}
	if up, ok := Number(f.Uptime); ok {
		b.add("sys.uptime", "Uptime", model.Info, (time.Duration(up) * time.Second).String(), "")
	}

	// ── Security ────────────────────────────────────────────────────────
	b.group = "Security"
	if f.Hostname == "raspberrypi" {
		b.add("sec.hostname", "Hostname", model.Warn, "raspberrypi",
			"Default hostname: two Pis on one LAN collide on raspberrypi.local (mDNS).")
	} else if f.Hostname != "" {
		b.add("sec.hostname", "Hostname", model.Pass, f.Hostname, "")
	}
	if hasUser(f.Passwd, "pi") {
		b.add("sec.piuser", "Default user 'pi'", model.Warn, "exists",
			"The well-known 'pi' user is the first thing bots try over SSH. Use your own user, or at least make sure the password is not 'raspberry'.")
	} else {
		b.add("sec.piuser", "Default user 'pi'", model.Pass, "absent", "")
	}
	if f.SSHActive == "active" {
		sd := sshd(f.SSHDConfig)
		switch {
		case sd["permitrootlogin"] == "yes":
			b.add("sec.ssh", "SSH", model.Fail, "root login allowed", "Set PermitRootLogin no.")
		case sd["passwordauthentication"] == "yes":
			b.add("sec.ssh", "SSH", model.Warn, "password login allowed",
				"Use keys and set PasswordAuthentication no — especially if port 22 is reachable from outside.")
		case len(sd) == 0:
			b.add("sec.ssh", "SSH", model.Info, "active (sshd -T needs root)", "")
		default:
			b.add("sec.ssh", "SSH", model.Pass, "keys only", "")
		}
	}

	// ── Network ─────────────────────────────────────────────────────────
	b.group = "Network"
	if gw := probe.Gateway(f.DefaultRoute); gw == "" {
		b.add("net.route", "Default route", model.Fail, "none", "No default gateway: no updates, no NTP.")
	} else {
		b.add("net.route", "Default route", model.Pass, "via "+gw+" dev "+devOf(f.DefaultRoute), "")
		loss, avg, ok := parsePing(f.Ping)
		switch {
		case !ok:
			b.add("net.ping", "Gateway ping", model.Skip, "no ping output", "")
		case loss > 0:
			b.add("net.ping", "Gateway ping", model.Warn, fmt.Sprintf("%.0f %% loss, avg %.1f ms", loss, avg),
				"Packet loss to the router. On Wi-Fi: check power saving and signal; on Ethernet: cable/switch port.")
		case avg > e.PingWarnMs:
			b.add("net.ping", "Gateway ping", model.Warn, fmt.Sprintf("avg %.1f ms", avg), "Slow round trip to the router; Wi-Fi power saving is the usual culprit.")
		default:
			b.add("net.ping", "Gateway ping", model.Pass, fmt.Sprintf("0 %% loss, avg %.1f ms", avg), "")
		}
	}
	if f.WifiPresent {
		if strings.Contains(f.WifiPowerSave, "on") {
			b.add("net.wifips", "Wi-Fi power saving", model.Warn, "on",
				"Brcmfmac power saving causes lag and dropped SSH sessions on headless Pis. Disable it (NetworkManager: wifi.powersave = 2).")
		} else if f.WifiPowerSave != "" {
			b.add("net.wifips", "Wi-Fi power saving", model.Pass, "off", "")
		}
		if strings.Contains(f.WifiCountry, "country 00") {
			b.add("net.country", "Wi-Fi country", model.Warn, "not set (00)",
				"Without a country code channels 12/13 and 5 GHz may be unavailable or Wi-Fi stays blocked. `raspi-config nonint do_wifi_country DE`.")
		} else if c := between(f.WifiCountry, "country ", ":"); c != "" {
			b.add("net.country", "Wi-Fi country", model.Pass, c, "")
		}
		if strings.Contains(f.RFKill, "Soft blocked: yes") {
			b.add("net.rfkill", "rfkill", model.Warn, "a radio is soft-blocked", "`rfkill unblock wifi` — happens on fresh images without a Wi-Fi country.")
		}
	}
	return b.out
}

// StressResult judges the samples of the stress phase.
func StressResult(samples []model.Sample, e Expect, nominalMHz float64) []model.Result {
	if len(samples) == 0 {
		return nil
	}
	b := &builder{group: "Stress test"}
	var peak, minMHz float64 = 0, 1e9
	var flags uint64
	for _, s := range samples {
		if s.TempC > peak {
			peak = s.TempC
		}
		if s.ArmMHz > 0 && s.ArmMHz < minMHz {
			minMHz = s.ArmMHz
		}
		flags |= s.Throttled & 0xf
	}
	if nominalMHz == 0 {
		for _, s := range samples {
			nominalMHz = max(nominalMHz, s.ArmMHz)
		}
	}
	dur := samples[len(samples)-1].At.Sub(samples[0].At).Round(time.Second)
	st, hint := model.Pass, ""
	if peak >= e.StressMaxC {
		st, hint = model.Fail, "Too hot under full load: it will throttle in real workloads. Improve cooling."
	} else if peak >= e.TempWarnC+10 {
		st, hint = model.Warn, "Getting close to the throttle point under load."
	}
	b.add("stress.temp", "Peak temperature under load", st, fmt.Sprintf("%.1f °C over %s", peak, dur), hint)
	if flags != 0 {
		b.add("stress.throttle", "Throttling during load", model.Fail, strings.Join(ThrottleText(flags), ", "),
			"Throttling under load means either the PSU (under-voltage) or cooling is insufficient.")
	} else {
		b.add("stress.throttle", "Throttling during load", model.Pass, "none", "")
	}
	if minMHz < 1e9 {
		st := model.Pass
		hint := ""
		if nominalMHz > 0 && minMHz < nominalMHz*0.9 {
			st, hint = model.Warn, "The ARM clock dropped below its nominal speed under load."
		}
		b.add("stress.clock", "Lowest ARM clock under load", st, fmt.Sprintf("%.0f MHz", minMHz), hint)
	}
	return b.out
}

// StorageResult judges the storage phase.
func StorageResult(writeMBs, readMBs float64, ok bool, size string, e Expect) []model.Result {
	b := &builder{group: "Storage test"}
	if !ok {
		b.add("io.verify", "Read-back verification", model.Fail, "DATA MISMATCH",
			"Data read back differs from what was written: counterfeit card (fake capacity) or a dying card. Replace it now.")
	} else {
		b.add("io.verify", "Read-back verification", model.Pass, size+" identical (SHA-256)", "")
	}
	st, hint := model.Pass, ""
	if writeMBs < e.MinWriteMBs {
		st, hint = model.Warn, "Below Class 10 / U1. Use an A1/A2 card (SanDisk, Samsung) or boot from USB/NVMe SSD."
	}
	b.add("io.write", "Sequential write (fsync)", st, fmt.Sprintf("%.1f MB/s", writeMBs), hint)
	st, hint = model.Pass, ""
	if readMBs < e.MinReadMBs {
		st, hint = model.Warn, "Slow reads. Pi 4/5 support SDR104 (~45–90 MB/s) with good cards."
	}
	b.add("io.read", "Sequential read", st, fmt.Sprintf("%.1f MB/s", readMBs), hint)
	return b.out
}

// ── helpers ────────────────────────────────────────────────────────────

var sdVendors = map[string]string{
	"0x000001": "Panasonic", "0x000002": "Toshiba/Kioxia", "0x000003": "SanDisk", "0x00001b": "Samsung",
	"0x00001d": "ADATA", "0x000027": "Phison", "0x000028": "Lexar", "0x000031": "Silicon Power",
	"0x000041": "Kingston", "0x000074": "Transcend", "0x000076": "Patriot", "0x000082": "Sony",
	"0x00009c": "Angelbird/Hoodman", "0x0000ad": "Longsys/Raspberry Pi",
}

type logPattern struct {
	id, title, needle, hint string
	st                      model.Status
}

var logPatterns = []logPattern{
	{"log.undervolt", "Under-voltage events", "Under-voltage detected", "The firmware saw the supply drop below ~4.63 V. Replace PSU/cable.", model.Fail},
	{"log.mmc", "SD/MMC errors", "mmc0: ", "Timeouts or CRC errors on the SD bus: bad card, bad contacts or under-voltage.", model.Fail},
	{"log.ext4", "Filesystem errors", "EXT4-fs error", "The filesystem is damaged. Run fsck from another machine and replace the card.", model.Fail},
	{"log.io", "Block I/O errors", "I/O error", "Read/write failures on a block device (SD or USB disk).", model.Fail},
	{"log.oom", "Out-of-memory kills", "Out of memory", "Processes were killed for lack of RAM. Add zram or reduce workload.", model.Warn},
	{"log.usb", "USB resets / over-current", "over-current", "A USB device draws too much current. Use a powered hub.", model.Warn},
	{"log.uas", "UAS adapter trouble", "uas_eh_abort_handler", "Your USB-SATA adapter has broken UAS support. Add usb-storage.quirks=<vid>:<pid>:u to cmdline.txt.", model.Warn},
	{"log.thermal", "Thermal events", "thermal", "Thermal messages in the log; check cooling.", model.Info},
}

func logChecks(log, group string) []model.Result {
	if log == "" {
		return []model.Result{{ID: "log.read", Group: group, Title: "Kernel log", Status: model.Skip, Value: "not readable", Hint: "Run as root to read the kernel log."}}
	}
	var out []model.Result
	bad := false
	for _, p := range logPatterns {
		n, first := 0, ""
		for _, l := range strings.Split(log, "\n") {
			if strings.Contains(l, p.needle) && !(p.id == "log.mmc" && !isMMCError(l)) {
				if n == 0 {
					first = strings.TrimSpace(l)
				}
				n++
			}
		}
		if n > 0 {
			bad = bad || p.st == model.Fail || p.st == model.Warn
			out = append(out, model.Result{ID: p.id, Group: group, Title: p.title, Status: p.st,
				Value: fmt.Sprintf("%d× e.g. %s", n, clip(first, 70)), Hint: p.hint})
		}
	}
	if !bad {
		out = append([]model.Result{{ID: "log.clean", Group: group, Title: "Known error patterns", Status: model.Pass, Value: "none found"}}, out...)
	}
	return out
}

func isMMCError(l string) bool {
	for _, w := range []string{"timeout", "error", "CRC", "Timeout"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// expandCheck warns when the root partition uses much less than the device.
func expandCheck(src, lsblk, group string) []model.Result {
	name := strings.TrimPrefix(src, "/dev/")
	var part, disk *blk
	all := parseLsblk(lsblk)
	for i := range all {
		if all[i].Name == name {
			part = &all[i]
		}
	}
	if part == nil {
		return nil
	}
	for i := range all {
		if all[i].Name == part.Parent {
			disk = &all[i]
		}
	}
	if disk == nil || disk.Size == 0 {
		return nil
	}
	var used uint64
	for _, p := range all {
		if p.Parent == disk.Name && p.Type == "part" {
			used += p.Size
		}
	}
	free := disk.Size - min(used, disk.Size)
	r := model.Result{ID: "disk.expand", Group: group, Title: "Root partition expanded",
		Value: fmt.Sprintf("%s partition on %s device", human(part.Size), human(disk.Size))}
	if free > disk.Size/10 && free > 1<<30 {
		r.Status = model.Warn
		r.Value += fmt.Sprintf(" — %s unallocated", human(free))
		r.Hint = "The filesystem was never expanded (image written with dd, first-boot resize skipped). `sudo raspi-config nonint do_expand_rootfs` and reboot."
	} else {
		r.Status = model.Pass
	}
	return []model.Result{r}
}

func parsePing(s string) (loss, avg float64, ok bool) {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "packet loss") {
			for _, part := range strings.Split(l, ",") {
				if strings.Contains(part, "packet loss") {
					loss, _ = Number(part)
				}
			}
			ok = true
		}
		if strings.Contains(l, "min/avg/max") {
			_, v, _ := strings.Cut(l, "=")
			fs := strings.Split(strings.TrimSpace(v), "/")
			if len(fs) >= 2 {
				avg, _ = strconv.ParseFloat(fs[1], 64)
			}
		}
	}
	return
}

func sshd(t string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(t, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), " ")
		if ok {
			m[strings.ToLower(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func confValue(t, key string) string {
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, key+"=") {
			return strings.TrimPrefix(l, key+"=")
		}
	}
	return ""
}

func hasUser(passwd, u string) bool {
	for _, l := range strings.Split(passwd, "\n") {
		if strings.HasPrefix(l, u+":") {
			return true
		}
	}
	return false
}

func meaningfulLines(t string) int {
	n := 0
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			n++
		}
	}
	return n
}

func unitNames(t string) string {
	var n []string
	for _, l := range strings.Split(t, "\n") {
		if fs := strings.Fields(l); len(fs) > 0 {
			n = append(n, fs[0])
		}
	}
	return strings.Join(n, ", ")
}

func devOf(route string) string {
	fs := strings.Fields(route)
	for i := 0; i+1 < len(fs); i++ {
		if fs[i] == "dev" {
			return fs[i+1]
		}
	}
	return "?"
}

func between(s, a, b string) string {
	_, r, ok := strings.Cut(s, a)
	if !ok {
		return ""
	}
	r, _, _ = strings.Cut(r, b)
	return strings.TrimSpace(r)
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return clip(l, 60) }
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

// ConfigArmFreq returns arm_freq from config.txt, if set.
func ConfigArmFreq(cfg string) (float64, bool) {
	v, ok := configValue(cfg, "arm_freq")
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	return f, err == nil
}
