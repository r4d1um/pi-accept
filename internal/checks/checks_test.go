package checks

import (
	"testing"

	"github.com/r4d1um/pi-accept/internal/model"
	"github.com/r4d1um/pi-accept/internal/probe"
)

func TestParseBoard(t *testing.T) {
	b, ok := ParseBoard("Revision\t: 2c03114\n")
	if !ok || b.TypeName != "4B" || b.MemMB != 4096 || !b.Warranty || b.Family() != "pi4" {
		t.Fatalf("got %+v", b)
	}
	b, _ = ParseBoard("Revision\t: d04170\n")
	if b.TypeName != "5" || b.MemMB != 8192 || b.Warranty || b.Family() != "pi5" {
		t.Fatalf("got %+v", b)
	}
}

func TestThrottled(t *testing.T) {
	n, ok := Throttled("throttled=0x50005")
	if !ok || n != 0x50005 || len(ThrottleText(n)) != 4 {
		t.Fatalf("got %x %v", n, ThrottleText(n))
	}
}

func verdictOf(t *testing.T, fake string) (model.Status, map[string]model.Status) {
	t.Helper()
	rs := Evaluate(probe.Gather(probe.Fake{Dir: "../../testdata/fakes/" + fake}), DefaultExpect())
	m := map[string]model.Status{}
	for _, r := range rs {
		m[r.ID] = r.Status
	}
	return model.Verdict(rs), m
}

func TestFakes(t *testing.T) {
	if v, _ := verdictOf(t, "pi5-good"); v != model.Pass {
		t.Errorf("pi5-good verdict %v", v)
	}
	v, m := verdictOf(t, "pi4-tired")
	if v != model.Fail {
		t.Errorf("pi4-tired verdict %v", v)
	}
	for id, want := range map[string]model.Status{
		"power.throttled": model.Fail, "log.undervolt": model.Fail, "log.ext4": model.Fail, "sys.ntp": model.Fail,
		"disk.expand": model.Warn, "os.arch": model.Warn, "fw.legacycfg": model.Warn, "disk.sd": model.Warn,
		"net.wifips": model.Warn, "net.country": model.Warn, "sec.piuser": model.Warn, "board.warranty": model.Warn,
	} {
		if m[id] != want {
			t.Errorf("%s = %v, want %v", id, m[id], want)
		}
	}
}
