// Package phases runs the active tests (CPU stress, storage write/read-back).
package phases

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/r4d1um/pi-accept/internal/checks"
	"github.com/r4d1um/pi-accept/internal/model"
	"github.com/r4d1um/pi-accept/internal/probe"
)

// Sample reads temperature, clock and throttle flags once.
func Sample(s probe.Source) model.Sample {
	sm := model.Sample{At: time.Now().UTC()}
	if v, err := s.Run("vcgencmd", "measure_temp"); err == nil {
		sm.TempC, _ = checks.Number(v)
	} else if v, err := s.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		if n, ok := checks.Number(v); ok {
			sm.TempC = n / 1000
		}
	}
	if v, err := s.Run("vcgencmd", "measure_clock", "arm"); err == nil {
		if n, ok := checks.Number(v); ok {
			sm.ArmMHz = n / 1e6
		}
	}
	if v, err := s.Run("vcgencmd", "get_throttled"); err == nil {
		sm.Throttled, _ = checks.Throttled(v)
	}
	return sm
}

// Stress burns all cores for d and calls onSample every interval.
// With a fake source it replays <fake>/sim.json instead (fast demo).
func Stress(s probe.Source, d, interval time.Duration, onSample func(model.Sample, float64)) []model.Sample {
	if f, ok := s.(probe.Fake); ok {
		return replay(f, d, interval, onSample)
	}
	var stop atomic.Bool
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			x := 1.0001
			for !stop.Load() {
				for j := 0; j < 1e5; j++ {
					x = x*x - x/3.7 + 0.1
				}
			}
			_ = x
		}()
	}
	defer stop.Store(true)
	var out []model.Sample
	start := time.Now()
	for t := time.NewTicker(interval); ; <-t.C {
		sm := Sample(s)
		out = append(out, sm)
		el := time.Since(start)
		onSample(sm, min(1, el.Seconds()/d.Seconds()))
		if el >= d {
			t.Stop()
			return out
		}
	}
}

func replay(f probe.Fake, d, interval time.Duration, onSample func(model.Sample, float64)) []model.Sample {
	var sim []model.Sample
	b, err := os.ReadFile(filepath.Join(f.Dir, "sim.json"))
	if err != nil || json.Unmarshal(b, &sim) != nil || len(sim) == 0 {
		return nil
	}
	base := time.Now().UTC()
	for i := range sim {
		sim[i].At = base.Add(time.Duration(i) * (d / time.Duration(len(sim))))
		onSample(sim[i], float64(i+1)/float64(len(sim)))
		time.Sleep(interval)
	}
	return sim
}

// StorageOutcome is the measured result of the storage phase.
type StorageOutcome struct {
	WriteMBs, ReadMBs float64
	Match             bool
	CacheDropped      bool
}

// Storage writes sizeMB of pseudo-random data into dir, fsyncs, drops the page
// cache (root only) and reads it back comparing SHA-256. Progress 0..1.
func Storage(dir string, sizeMB int, progress func(stage string, p float64)) (StorageOutcome, error) {
	var out StorageOutcome
	tmp := filepath.Join(dir, ".piaccept-io.tmp")
	fh, err := os.Create(tmp)
	if err != nil {
		return out, err
	}
	defer os.Remove(tmp)

	const chunk = 4 << 20
	buf := make([]byte, chunk)
	rng := rand.NewChaCha8([32]byte{'p', 'i'})
	wsum := sha256.New()
	n := sizeMB / 4
	if n < 1 {
		n = 1
	}
	start := time.Now()
	for i := 0; i < n; i++ {
		for j := 0; j+8 <= len(buf); j += 8 { // go1.22: ChaCha8 has no Read yet
			binary.LittleEndian.PutUint64(buf[j:], rng.Uint64())
		}
		wsum.Write(buf)
		if _, err := fh.Write(buf); err != nil {
			fh.Close()
			return out, err
		}
		progress("write", float64(i+1)/float64(n))
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		return out, err
	}
	fh.Close()
	total := float64(n*chunk) / 1e6
	out.WriteMBs = total / time.Since(start).Seconds()

	out.CacheDropped = os.WriteFile("/proc/sys/vm/drop_caches", []byte("3"), 0) == nil

	rh, err := os.Open(tmp)
	if err != nil {
		return out, err
	}
	defer rh.Close()
	rsum := sha256.New()
	start = time.Now()
	for i := 0; ; i++ {
		m, err := io.ReadFull(rh, buf)
		rsum.Write(buf[:m])
		progress("read", float64(i+1)/float64(n))
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return out, err
		}
	}
	out.ReadMBs = total / time.Since(start).Seconds()
	out.Match = string(wsum.Sum(nil)) == string(rsum.Sum(nil))
	return out, nil
}
