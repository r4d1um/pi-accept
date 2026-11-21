// Package ui renders the fancy terminal output (truecolor, Unicode, live lines).
// Falls back to plain text with NO_COLOR or when stdout is not a terminal.
package ui

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/r4d1um/pi-accept/internal/model"
)

type UI struct {
	W     io.Writer
	Color bool
	Live  bool // \r redraws allowed
	Width int
}

func New(w *os.File, forceColor bool) *UI {
	tty := false
	if st, err := w.Stat(); err == nil {
		tty = st.Mode()&os.ModeCharDevice != 0
	}
	color := (tty || forceColor) && os.Getenv("NO_COLOR") == ""
	return &UI{W: w, Color: color, Live: tty || forceColor, Width: 84}
}

// ── colour helpers ──────────────────────────────────────────────────────

type rgb struct{ r, g, b int }

var (
	cPass  = rgb{80, 220, 120}
	cWarn  = rgb{255, 190, 60}
	cFail  = rgb{255, 85, 95}
	cInfo  = rgb{110, 170, 255}
	cSkip  = rgb{140, 140, 150}
	cDim   = rgb{120, 125, 140}
	cTitle = rgb{235, 235, 245}
	gradA  = rgb{255, 60, 120} // raspberry
	gradB  = rgb{120, 90, 255} // violet
	gradC  = rgb{40, 210, 200} // teal
)

func (u *UI) fg(c rgb, s string) string {
	if !u.Color {
		return s
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", c.r, c.g, c.b, s)
}
func (u *UI) bold(s string) string {
	if !u.Color {
		return s
	}
	return "\x1b[1m" + s + "\x1b[22m"
}

func lerp(a, b rgb, t float64) rgb {
	return rgb{a.r + int(float64(b.r-a.r)*t), a.g + int(float64(b.g-a.g)*t), a.b + int(float64(b.b-a.b)*t)}
}
func grad3(t float64) rgb {
	if t < 0.5 {
		return lerp(gradA, gradB, t*2)
	}
	return lerp(gradB, gradC, (t-0.5)*2)
}

// gradient colours each rune of s along the raspberry→violet→teal ramp.
func (u *UI) gradient(s string, offset float64) string {
	if !u.Color {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		t := math.Mod(float64(i)/float64(max(1, len(rs)-1))+offset, 1)
		c := grad3(t)
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm%c", c.r, c.g, c.b, r)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func statusColor(s model.Status) rgb {
	return [...]rgb{cPass, cInfo, cWarn, cFail, cSkip}[s]
}
func icon(s model.Status) string { return [...]string{"✔", "ℹ", "⚠", "✖", "○"}[s] }

// ── big letters ─────────────────────────────────────────────────────────

var font = map[rune][5]string{
	'P': {"█████╗ ", "██╔═██╗", "█████╔╝", "██╔══╝ ", "██║    "},
	'I': {"██╗", "██║", "██║", "██║", "██║"},
	'A': {" ████╗ ", "██╔═██╗", "██████║", "██╔═██║", "██║ ██║"},
	'C': {" █████╗", "██╔═══╝", "██║    ", "██║    ", "╚█████╗"},
	'E': {"██████╗", "██╔═══╝", "█████╗ ", "██╔══╝ ", "██████╗"},
	'T': {"██████╗", "╚═██╔═╝", "  ██║  ", "  ██║  ", "  ██║  "},
	'S': {" █████╗", "██╔═══╝", "╚████╗ ", " ╚══██╗", "█████╔╝"},
	'W': {"██╗    ██╗", "██║    ██║", "██║ █╗ ██║", "██║███╗██║", "╚███╔███╔╝"},
	'R': {"█████╗ ", "██╔═██╗", "█████╔╝", "██╔═██╗", "██║ ██║"},
	'N': {"███╗  ██╗", "████╗ ██║", "██╔██╗██║", "██║╚████║", "██║ ╚███║"},
	'F': {"██████╗", "██╔═══╝", "█████╗ ", "██╔══╝ ", "██║    "},
	'L': {"██╗    ", "██║    ", "██║    ", "██║    ", "██████╗"},
	' ': {"  ", "  ", "  ", "  ", "  "},
}

func big(word string) [5]string {
	var rows [5]string
	for _, r := range word {
		g := font[r]
		for i := range rows {
			rows[i] += g[i] + " "
		}
	}
	return rows
}

// Banner prints the animated title (a short colour sweep on a live terminal).
func (u *UI) Banner(version, host, model string, fake bool) {
	rows := big("PI ACCEPT")
	frames := 1
	if u.Live && u.Color {
		frames = 14
	}
	for f := 0; f < frames; f++ {
		if f > 0 {
			fmt.Fprintf(u.W, "\x1b[%dA", len(rows))
		}
		off := float64(f) / float64(frames)
		for i, r := range rows {
			fmt.Fprintln(u.W, "  "+u.gradient(r, off+float64(i)*0.03))
		}
		if frames > 1 {
			time.Sleep(45 * time.Millisecond)
		}
	}
	sub := fmt.Sprintf("Raspberry Pi acceptance test v%s · %s · %s UTC", version, host, time.Now().UTC().Format("2006-01-02 15:04"))
	fmt.Fprintln(u.W, "  "+u.fg(cDim, sub))
	if model != "" {
		fmt.Fprintln(u.W, "  "+u.bold(u.fg(cTitle, "🍓 "+model)))
	}
	if fake {
		fmt.Fprintln(u.W, "  "+u.fg(cWarn, "◆ FAKE MODE — sample data, not this machine"))
	}
	fmt.Fprintln(u.W)
}

// Spinner shows a spinner with msg until done is closed.
func (u *UI) Spinner(msg string, done <-chan struct{}) {
	if !u.Live {
		fmt.Fprintln(u.W, "  "+msg)
		<-done
		return
	}
	fr := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	for i := 0; ; i++ {
		select {
		case <-done:
			fmt.Fprintf(u.W, "\r\x1b[2K")
			return
		case <-time.After(80 * time.Millisecond):
			fmt.Fprintf(u.W, "\r  %s %s", u.fg(grad3(float64(i%20)/20), string(fr[i%len(fr)])), u.fg(cDim, msg))
		}
	}
}

// Section prints a group header with a gradient rule.
func (u *UI) Section(title string) {
	rule := strings.Repeat("─", max(4, u.Width-6-utf8.RuneCountInString(title)))
	fmt.Fprintf(u.W, "\n  %s %s %s\n", u.fg(gradA, "▌"), u.bold(u.fg(cTitle, title)), u.gradient(rule, 0))
}

// Row prints one result line.
func (u *UI) Row(r model.Result) {
	c := statusColor(r.Status)
	title := pad(r.Title, 32)
	val := r.Value
	if utf8.RuneCountInString(val) > u.Width-42 {
		val = string([]rune(val)[:u.Width-43]) + "…"
	}
	tag := u.fg(c, u.bold(icon(r.Status)+" "+pad(r.Status.String(), 4)))
	fmt.Fprintf(u.W, "    %s  %s %s\n", tag, u.fg(cTitle, title), u.fg(valColor(r.Status), val))
}

func valColor(s model.Status) rgb {
	if s == model.Warn || s == model.Fail {
		return statusColor(s)
	}
	return cDim
}

// ── live stress line ────────────────────────────────────────────────────

var sparks = []rune("▁▂▃▄▅▆▇█")

// StressLine redraws the live stress status line.
func (u *UI) StressLine(hist []model.Sample, p float64, d time.Duration) {
	if len(hist) == 0 {
		return
	}
	cur := hist[len(hist)-1]
	bar := u.bar(p, 22)
	var sp strings.Builder
	start := max(0, len(hist)-16) // the whole line stays within u.Width, or \r redraws wrap and stack up
	for _, s := range hist[start:] {
		t := math.Max(0, math.Min(1, (s.TempC-35)/50))
		ch := string(sparks[int(t*float64(len(sparks)-1))])
		sp.WriteString(u.fg(heat(t), ch))
	}
	flag := u.fg(cPass, "no throttle")
	if cur.Throttled&0xf != 0 {
		txt := "throttled" // short name of the most important bit; the full list is in the results below
		for _, b := range []struct {
			bit  uint64
			name string
		}{{0x1, "under-voltage"}, {0x4, "throttled"}, {0x2, "freq capped"}, {0x8, "soft temp limit"}} {
			if cur.Throttled&b.bit != 0 {
				txt = b.name
				break
			}
		}
		flag = u.fg(cFail, "⚡ "+txt)
	}
	line := fmt.Sprintf("  🔥 %s %s %s %s %s %s",
		bar, u.fg(cTitle, fmt.Sprintf("%3.0f%%", p*100)),
		u.fg(heat(math.Max(0, math.Min(1, (cur.TempC-35)/50))), fmt.Sprintf("%5.1f°C", cur.TempC)),
		pad(sp.String(), 0), u.fg(cDim, fmt.Sprintf("%4.0f MHz", cur.ArmMHz)), flag)
	if u.Live {
		fmt.Fprintf(u.W, "\r\x1b[2K%s", line)
	} else if p >= 1 {
		fmt.Fprintln(u.W, line)
	}
	if p >= 1 && u.Live {
		fmt.Fprintln(u.W)
	}
}

// Progress redraws a generic progress line (storage phase).
func (u *UI) Progress(label string, p float64) {
	line := fmt.Sprintf("  💾 %s %s %s", u.bar(p, 30), u.fg(cTitle, fmt.Sprintf("%3.0f%%", p*100)), u.fg(cDim, label))
	if u.Live {
		fmt.Fprintf(u.W, "\r\x1b[2K%s", line)
	}
}

func (u *UI) EndProgress() {
	if u.Live {
		fmt.Fprintf(u.W, "\r\x1b[2K")
	}
}

func heat(t float64) rgb {
	if t < 0.5 {
		return lerp(gradC, cWarn, t*2)
	}
	return lerp(cWarn, cFail, (t-0.5)*2)
}

func (u *UI) bar(p float64, w int) string {
	full := int(p * float64(w))
	var b strings.Builder
	for i := 0; i < w; i++ {
		if i < full {
			b.WriteString(u.fg(grad3(float64(i)/float64(w)), "█"))
		} else {
			b.WriteString(u.fg(rgb{55, 58, 70}, "░"))
		}
	}
	return b.String()
}

// ── summary ─────────────────────────────────────────────────────────────

// Pitfalls prints a box with every WARN/FAIL and its hint.
func (u *UI) Pitfalls(rs []model.Result) {
	var bad []model.Result
	for _, s := range []model.Status{model.Fail, model.Warn} {
		for _, r := range rs {
			if r.Status == s && r.Hint != "" {
				bad = append(bad, r)
			}
		}
	}
	if len(bad) == 0 {
		return
	}
	w := u.Width - 4
	fmt.Fprintln(u.W)
	fmt.Fprintln(u.W, "  "+u.fg(gradB, "╭─ ")+u.bold(u.fg(cTitle, "🕳  Pitfalls found & how to fix them "))+u.fg(gradB, strings.Repeat("─", w-41)+"╮"))
	for i, r := range bad {
		c := statusColor(r.Status)
		head := fmt.Sprintf("%2d. %s %s", i+1, icon(r.Status), r.Title)
		u.boxLine(u.fg(c, u.bold(head)), utf8.RuneCountInString(head), w)
		for _, l := range wrap(r.Hint, w-10) {
			u.boxLine("      "+u.fg(cTitle, l), 6+utf8.RuneCountInString(l), w)
		}
		if i < len(bad)-1 {
			u.boxLine("", 0, w)
		}
	}
	fmt.Fprintln(u.W, "  "+u.fg(gradB, "╰"+strings.Repeat("─", w-2)+"╯"))
}

func (u *UI) boxLine(s string, visible, w int) {
	fmt.Fprintf(u.W, "  %s %s%s %s\n", u.fg(gradB, "│"), s, strings.Repeat(" ", max(0, w-4-visible)), u.fg(gradB, "│"))
}

// Verdict prints the big result word and the counters.
func (u *UI) Verdict(v model.Status, rs []model.Result, took time.Duration, reportDir string) {
	cnt := map[model.Status]int{}
	for _, r := range rs {
		cnt[r.Status]++
	}
	word := map[model.Status]string{model.Pass: "PASS", model.Warn: "WARN", model.Fail: "FAIL"}[v]
	c := statusColor(v)
	fmt.Fprintln(u.W)
	for _, r := range big(word) {
		fmt.Fprintln(u.W, "    "+u.fg(c, r))
	}
	total := len(rs)
	var bar strings.Builder
	for _, s := range []model.Status{model.Pass, model.Info, model.Warn, model.Fail, model.Skip} {
		n := int(math.Round(float64(cnt[s]) / float64(max(1, total)) * 50))
		bar.WriteString(u.fg(statusColor(s), strings.Repeat("■", n)))
	}
	fmt.Fprintln(u.W, "    "+bar.String())
	fmt.Fprintf(u.W, "    %s  %s  %s  %s  %s   %s\n",
		u.fg(cPass, fmt.Sprintf("✔ %d pass", cnt[model.Pass])), u.fg(cInfo, fmt.Sprintf("ℹ %d info", cnt[model.Info])),
		u.fg(cWarn, fmt.Sprintf("⚠ %d warn", cnt[model.Warn])), u.fg(cFail, fmt.Sprintf("✖ %d fail", cnt[model.Fail])),
		u.fg(cSkip, fmt.Sprintf("○ %d skip", cnt[model.Skip])), u.fg(cDim, "in "+took.Round(100*time.Millisecond).String()))
	if reportDir != "" {
		fmt.Fprintln(u.W, "    "+u.fg(cDim, "📄 report: "+reportDir+"/report.{json,md}"))
	}
	fmt.Fprintln(u.W)
}

// ── text helpers ────────────────────────────────────────────────────────

func pad(s string, n int) string {
	if k := utf8.RuneCountInString(s); k < n {
		return s + strings.Repeat(" ", n-k)
	}
	return s
}

func wrap(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > w {
			out = append(out, line)
			line = word
		} else if line == "" {
			line = word
		} else {
			line += " " + word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
