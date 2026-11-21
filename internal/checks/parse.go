package checks

import (
	"regexp"
	"strconv"
	"strings"
)

// Board is the decoded new-style revision code (cpuinfo "Revision").
type Board struct {
	Raw         uint64
	NewStyle    bool
	Type        int
	TypeName    string
	Processor   string
	MemMB       int
	Warranty    bool // bit 25 set: warranty void (force_turbo + over_voltage once used)
	Overvoltage bool // bit 31: over-voltage disallowed
}

var typeNames = map[int]string{
	0x00: "A", 0x01: "B", 0x02: "A+", 0x03: "B+", 0x04: "2B", 0x06: "CM1", 0x08: "3B",
	0x09: "Zero", 0x0a: "CM3", 0x0c: "Zero W", 0x0d: "3B+", 0x0e: "3A+", 0x10: "CM3+",
	0x11: "4B", 0x12: "Zero 2 W", 0x13: "400", 0x14: "CM4", 0x15: "CM4S", 0x17: "5",
	0x18: "CM5", 0x19: "500", 0x1a: "CM5 Lite",
}
var procNames = []string{"BCM2835", "BCM2836", "BCM2837", "BCM2711", "BCM2712"}
var memSizes = []int{256, 512, 1024, 2048, 4096, 8192, 16384}

func ParseBoard(cpuinfo string) (Board, bool) {
	v := field(cpuinfo, "Revision")
	if v == "" {
		return Board{}, false
	}
	n, err := strconv.ParseUint(v, 16, 64)
	if err != nil {
		return Board{}, false
	}
	b := Board{Raw: n, NewStyle: n&(1<<23) != 0}
	if !b.NewStyle {
		b.TypeName = "old-style (Pi 1)"
		b.MemMB = 256
		return b, true
	}
	b.Type = int(n>>4) & 0xff
	b.TypeName = typeNames[b.Type]
	if p := int(n>>12) & 0xf; p < len(procNames) {
		b.Processor = procNames[p]
	}
	if m := int(n>>20) & 0x7; m < len(memSizes) {
		b.MemMB = memSizes[m]
	}
	b.Warranty = n&(1<<25) != 0
	b.Overvoltage = n&(1<<31) != 0
	return b, true
}

// Family groups boards by power/thermal behaviour.
func (b Board) Family() string {
	switch b.Processor {
	case "BCM2712":
		return "pi5"
	case "BCM2711":
		return "pi4"
	case "BCM2837":
		if b.Type == 0x12 {
			return "zero2"
		}
		return "pi3"
	case "BCM2835":
		return "pi1/zero"
	}
	return "pi2"
}

// PSU recommendation per family (official supplies).
func (b Board) PSU() string {
	switch b.Family() {
	case "pi5":
		return "5.1 V / 5 A USB-C PD (27 W)"
	case "pi4":
		return "5.1 V / 3 A USB-C (15 W)"
	case "pi3":
		return "5.1 V / 2.5 A micro-USB"
	}
	return "5.1 V / 2.5 A"
}

// field returns the value of "Key : value" lines (cpuinfo, meminfo style).
func field(text, key string) string {
	for _, l := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// kv parses KEY=VALUE lines (os-release, timedatectl show), unquoting values.
func kv(text string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok {
			m[k] = strings.Trim(v, `"'`)
		}
	}
	return m
}

var numRe = regexp.MustCompile(`[-+]?[0-9]*\.?[0-9]+`)

// Number pulls the first number out of strings like "temp=48.3'C".
func Number(s string) (float64, bool) {
	_, after, ok := strings.Cut(s, "=")
	if !ok {
		after = s
	}
	m := numRe.FindString(after)
	if m == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(m, 64)
	return f, err == nil
}

// Throttled parses "throttled=0x50005".
func Throttled(s string) (uint64, bool) {
	_, v, ok := strings.Cut(strings.TrimSpace(s), "=")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(v, "0x"), 16, 64)
	return n, err == nil
}

// ThrottleText explains the get_throttled bit field.
func ThrottleText(n uint64) []string {
	names := map[uint]string{
		0: "under-voltage NOW", 1: "ARM frequency capped NOW", 2: "throttled NOW", 3: "soft temp limit NOW",
		16: "under-voltage occurred", 17: "frequency capping occurred", 18: "throttling occurred", 19: "soft temp limit occurred",
	}
	var out []string
	for _, b := range []uint{0, 1, 2, 3, 16, 17, 18, 19} {
		if n&(1<<b) != 0 {
			out = append(out, names[b])
		}
	}
	return out
}

// memKB reads a /proc/meminfo field in kB.
func memKB(meminfo, key string) int {
	v := strings.Fields(field(meminfo, key))
	if len(v) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(v[0])
	return n
}

// configHas reports whether config.txt sets key (optionally key=value) outside comments.
func configValue(cfg, key string) (string, bool) {
	val, found := "", false
	for _, l := range strings.Split(cfg, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "[") {
			continue
		}
		k, v, _ := strings.Cut(l, "=")
		if strings.TrimSpace(k) == key {
			val, found = strings.TrimSpace(v), true
		}
	}
	return val, found
}

func configLines(cfg, prefix string) []string {
	var out []string
	for _, l := range strings.Split(cfg, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

// lsblk -P line parser.
type blk struct {
	Name, Type, Parent string
	Size               uint64
}

var pairRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

func parseLsblk(s string) []blk {
	var out []blk
	for _, l := range strings.Split(s, "\n") {
		m := map[string]string{}
		for _, p := range pairRe.FindAllStringSubmatch(l, -1) {
			m[p[1]] = p[2]
		}
		if m["NAME"] == "" {
			continue
		}
		n, _ := strconv.ParseUint(m["SIZE"], 10, 64)
		out = append(out, blk{Name: m["NAME"], Type: m["TYPE"], Parent: m["PKNAME"], Size: n})
	}
	return out
}

func human(b uint64) string {
	const u = 1024
	if b < u {
		return strconv.FormatUint(b, 10) + " B"
	}
	div, exp := uint64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	return strconv.FormatFloat(float64(b)/float64(div), 'f', 1, 64) + " " + string("KMGTPE"[exp]) + "iB"
}
