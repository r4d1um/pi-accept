// Package probe does all I/O: it reads files and runs commands, either on the
// real Pi or from a fake directory (tests/fakes/<name>). No decisions here.
package probe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Source abstracts the machine so checks can run against sample data.
type Source interface {
	ReadFile(path string) (string, error)
	Exists(path string) bool
	Run(name string, args ...string) (string, error)
	Glob(pattern string) []string
}

// Real reads the live system.
type Real struct{ Timeout time.Duration }

func (Real) ReadFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}
func (Real) Exists(p string) bool { _, err := os.Stat(p); return err == nil }
func (Real) Glob(p string) []string {
	m, _ := filepath.Glob(p)
	return m
}
func (r Real) Run(name string, args ...string) (string, error) {
	t := r.Timeout
	if t == 0 {
		t = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), t)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// Fake serves <dir>/fs/<path> for files and <dir>/cmd/<key>.txt for commands.
type Fake struct{ Dir string }

func (f Fake) ReadFile(p string) (string, error) {
	b, err := os.ReadFile(filepath.Join(f.Dir, "fs", p))
	return string(b), err
}
func (f Fake) Exists(p string) bool {
	_, err := os.Stat(filepath.Join(f.Dir, "fs", p))
	return err == nil
}
func (f Fake) Glob(p string) []string {
	root := filepath.Join(f.Dir, "fs")
	m, _ := filepath.Glob(filepath.Join(root, p))
	for i := range m {
		m[i] = strings.TrimPrefix(m[i], root)
	}
	return m
}
func (f Fake) Run(name string, args ...string) (string, error) {
	b, err := os.ReadFile(filepath.Join(f.Dir, "cmd", CmdKey(name, args...)+".txt"))
	if err != nil {
		return "", errors.New("not found (fake)")
	}
	return string(b), nil
}

// CmdKey maps a command line to a fake file name.
func CmdKey(name string, args ...string) string {
	s := strings.Join(append([]string{name}, args...), " ")
	return strings.NewReplacer("/", "_", " ", "_", ",", "_").Replace(s)
}

// Facts is the raw material for the checks. Strings are kept raw on purpose:
// parsing lives in package checks, where it is pure and testable.
type Facts struct {
	Fake            bool
	Model           string
	CPUInfo         string
	MemInfo         string
	OSRelease       string
	UnameM          string
	DpkgArch        string
	Hostname        string
	Throttled       string
	Temp            string
	Volts           string
	ArmClock        string
	GPUMem          string
	EEPROM          string
	FindmntRoot     string
	DFRoot          string
	Lsblk           string
	SDManfid        string
	SDName          string
	SDDate          string
	KernelLog       string
	Timedatectl     string
	WifiPresent     bool
	WifiPowerSave   string
	WifiCountry     string
	RFKill          string
	Passwd          string
	SSHActive       string
	SSHDConfig      string
	FailedUnits     string
	ConfigPath      string
	ConfigTxt       string
	LegacyConfigTxt string
	Watchdog        bool
	SystemdConf     string
	Swaps           string
	DefaultRoute    string
	Ping            string
	Uptime          string
	Missing         []string // commands/files that could not be read
}

// Gather collects all facts. Missing tools are recorded, never fatal.
func Gather(s Source) Facts {
	var f Facts
	_, f.Fake = s.(Fake)
	file := func(p string) string {
		v, err := s.ReadFile(p)
		if err != nil {
			f.Missing = append(f.Missing, p)
		}
		return strings.TrimRight(v, "\x00\n")
	}
	run := func(name string, args ...string) string {
		v, err := s.Run(name, args...)
		if err != nil && v == "" {
			f.Missing = append(f.Missing, strings.Join(append([]string{name}, args...), " "))
		}
		return strings.TrimSpace(v)
	}

	f.Model = file("/proc/device-tree/model")
	f.CPUInfo = file("/proc/cpuinfo")
	f.MemInfo = file("/proc/meminfo")
	f.OSRelease = file("/etc/os-release")
	f.Hostname = strings.TrimSpace(file("/etc/hostname"))
	f.Uptime = file("/proc/uptime")
	f.Swaps = file("/proc/swaps")
	f.Passwd = file("/etc/passwd")
	f.UnameM = run("uname", "-m")
	f.DpkgArch = run("dpkg", "--print-architecture")

	f.Throttled = run("vcgencmd", "get_throttled")
	f.Temp = run("vcgencmd", "measure_temp")
	f.Volts = run("vcgencmd", "measure_volts", "core")
	f.ArmClock = run("vcgencmd", "measure_clock", "arm")
	f.GPUMem = run("vcgencmd", "get_mem", "gpu")
	f.EEPROM = run("rpi-eeprom-update")

	f.FindmntRoot = run("findmnt", "-n", "-o", "SOURCE,FSTYPE,OPTIONS", "/")
	f.DFRoot = run("df", "-P", "-B1", "/")
	f.Lsblk = run("lsblk", "-b", "-n", "-P", "-o", "NAME,SIZE,TYPE,PKNAME")
	if s.Exists("/sys/block/mmcblk0/device/manfid") {
		f.SDManfid = strings.TrimSpace(file("/sys/block/mmcblk0/device/manfid"))
		f.SDName = strings.TrimSpace(file("/sys/block/mmcblk0/device/name"))
		f.SDDate = strings.TrimSpace(file("/sys/block/mmcblk0/device/date"))
	}

	f.KernelLog = run("journalctl", "-k", "-b", "--no-pager", "-q")
	if f.KernelLog == "" {
		f.KernelLog = run("dmesg")
	}
	f.Timedatectl = run("timedatectl", "show")

	if len(s.Glob("/sys/class/net/wlan*")) > 0 {
		f.WifiPresent = true
		f.WifiPowerSave = run("iw", "dev", "wlan0", "get", "power_save")
		f.WifiCountry = run("iw", "reg", "get")
		f.RFKill = run("rfkill", "list")
	}

	f.SSHActive = run("systemctl", "is-active", "ssh")
	f.SSHDConfig = run("sshd", "-T")
	f.FailedUnits = run("systemctl", "--failed", "--no-legend", "--plain")

	switch {
	case s.Exists("/boot/firmware/config.txt"):
		f.ConfigPath = "/boot/firmware/config.txt"
		f.LegacyConfigTxt, _ = s.ReadFile("/boot/config.txt")
	default:
		f.ConfigPath = "/boot/config.txt"
	}
	f.ConfigTxt = file(f.ConfigPath)

	f.Watchdog = s.Exists("/dev/watchdog")
	f.SystemdConf, _ = s.ReadFile("/etc/systemd/system.conf")

	f.DefaultRoute = run("ip", "-4", "route", "show", "default")
	if gw := Gateway(f.DefaultRoute); gw != "" {
		f.Ping = run("ping", "-c", "5", "-i", "0.2", "-W", "2", gw)
	}
	return f
}

// Gateway extracts the default gateway from `ip route show default`.
func Gateway(route string) string {
	fs := strings.Fields(route)
	for i := 0; i+1 < len(fs); i++ {
		if fs[i] == "via" {
			return fs[i+1]
		}
	}
	return ""
}
