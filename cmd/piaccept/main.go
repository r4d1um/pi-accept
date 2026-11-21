// piaccept — generalized Raspberry Pi acceptance test.
//
//	go run github.com/r4d1um/pi-accept/cmd/piaccept@latest --quick   # or a release binary piaccept-linux-arm64/-armv7/-armv6
//	sudo piaccept                       # static checks + 60 s stress + 256 MB storage test
//	sudo piaccept --stress 10m --storage 1024
//	piaccept --fake testdata/fakes/pi4-tired --stress 20s --interval 150ms
//
// Exit code: 0 PASS, 1 WARN, 2 FAIL.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/r4d1um/pi-accept/internal/checks"
	"github.com/r4d1um/pi-accept/internal/model"
	"github.com/r4d1um/pi-accept/internal/phases"
	"github.com/r4d1um/pi-accept/internal/probe"
	"github.com/r4d1um/pi-accept/internal/report"
	"github.com/r4d1um/pi-accept/internal/ui"
)

var version = "dev" // set by -ldflags "-X main.version=…" in the release build

func main() {
	e := checks.DefaultExpect()
	fake := flag.String("fake", "", "use a fake Pi directory (testdata/fakes/<name>) instead of this machine")
	stress := flag.Duration("stress", 60*time.Second, "CPU stress duration (0 = skip)")
	interval := flag.Duration("interval", 2*time.Second, "sample interval during stress")
	storage := flag.Int("storage", 256, "storage write/read-back test size in MB (0 = skip)")
	ioDir := flag.String("io-dir", "/var/tmp", "directory for the storage test (lives on the disk under test)")
	dir := flag.String("dir", "/var/lib/piaccept", "report directory (\"\" = no report)")
	quick := flag.Bool("quick", false, "static checks only (= --stress 0 --storage 0)")
	color := flag.Bool("color", false, "force colour even when not a terminal")
	flag.Float64Var(&e.TempWarnC, "temp-warn", e.TempWarnC, "idle temperature warning °C")
	flag.Float64Var(&e.TempFailC, "temp-fail", e.TempFailC, "idle temperature failure °C")
	flag.Float64Var(&e.StressMaxC, "stress-max", e.StressMaxC, "max temperature under load °C")
	flag.Float64Var(&e.DiskWarnPct, "disk-warn", e.DiskWarnPct, "root usage warning %")
	flag.Float64Var(&e.DiskFailPct, "disk-fail", e.DiskFailPct, "root usage failure %")
	flag.Float64Var(&e.MinWriteMBs, "min-write", e.MinWriteMBs, "minimum sequential write MB/s")
	flag.Float64Var(&e.MinReadMBs, "min-read", e.MinReadMBs, "minimum sequential read MB/s")
	flag.Float64Var(&e.PingWarnMs, "ping-warn", e.PingWarnMs, "gateway ping warning ms")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *quick {
		*stress, *storage = 0, 0
	}

	var src probe.Source = probe.Real{}
	if *fake != "" {
		src = probe.Fake{Dir: *fake}
	}
	u := ui.New(os.Stdout, *color)
	started := time.Now().UTC()

	done := make(chan struct{})
	var facts probe.Facts
	go func() { facts = probe.Gather(src); close(done) }()
	host := hostname(src)
	model0, _ := src.ReadFile("/proc/device-tree/model")
	u.Banner(version, host, strings.TrimRight(model0, "\x00\n"), *fake != "")
	if os.Geteuid() != 0 && *fake == "" {
		fmt.Print("  ⚠  not running as root: kernel log, sshd and cache drop are limited — use sudo\n\n")
	}
	u.Spinner("probing hardware, firmware, storage, network …", done)

	results := checks.Evaluate(facts, e)
	printGroups(u, results)

	var samples []model.Sample
	if *stress > 0 {
		u.Section(fmt.Sprintf("Stress test · all cores · %s", *stress))
		nominal := 0.0 // 0 = use the highest clock seen under load
		if v, ok := checks.ConfigArmFreq(facts.ConfigTxt); ok {
			nominal = v
		}
		samples = phases.Stress(src, *stress, *interval, func(s model.Sample, p float64) {
			samples = append(samples, s)
			u.StressLine(samples, p, *stress)
		})
		rs := checks.StressResult(samples, e, nominal)
		for _, r := range rs {
			u.Row(r)
		}
		results = append(results, rs...)
	}

	if *storage > 0 {
		u.Section(fmt.Sprintf("Storage test · %d MB write + read-back · %s", *storage, *ioDir))
		out, err := phases.Storage(*ioDir, *storage, func(stage string, p float64) { u.Progress(stage, p) })
		u.EndProgress()
		var rs []model.Result
		if err != nil {
			rs = []model.Result{{ID: "io.error", Group: "Storage test", Title: "Storage test", Status: model.Fail, Value: err.Error(),
				Hint: "Writing the test file failed: full disk, read-only filesystem or a dying card."}}
		} else {
			rs = checks.StorageResult(out.WriteMBs, out.ReadMBs, out.Match, fmt.Sprintf("%d MB", *storage), e)
			if !out.CacheDropped {
				rs = append(rs, model.Result{ID: "io.cache", Group: "Storage test", Title: "Page cache", Status: model.Info,
					Value: "not dropped (needs root) — read speed may be from RAM"})
			}
		}
		for _, r := range rs {
			u.Row(r)
		}
		results = append(results, rs...)
	}

	verdict := model.Verdict(results)
	u.Pitfalls(results)
	rep := model.Report{Schema: model.SchemaVersion, Version: version, Host: host, Model: facts.Model,
		Started: started, Finished: time.Now().UTC(), Results: results, Samples: samples, Verdict: verdict}
	written := ""
	if *dir != "" {
		if err := report.Write(*dir, rep); err != nil {
			fmt.Fprintf(os.Stderr, "  report not written: %v\n", err)
		} else {
			written = *dir
		}
	}
	u.Verdict(verdict, results, time.Since(started), written)
	os.Exit(map[model.Status]int{model.Pass: 0, model.Warn: 1, model.Fail: 2}[verdict])
}

func printGroups(u *ui.UI, rs []model.Result) {
	g := ""
	for _, r := range rs {
		if r.Group != g {
			g = r.Group
			u.Section(g)
		}
		u.Row(r)
	}
}

func hostname(s probe.Source) string {
	h, _ := s.ReadFile("/etc/hostname")
	if h = strings.TrimSpace(h); h == "" {
		h, _ = os.Hostname()
	}
	return h
}
