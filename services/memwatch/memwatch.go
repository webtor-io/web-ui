// Package memwatch captures pprof profiles at the moment of an abnormal
// heap spike. The web-ui memory incidents of 2026-08 are single requests
// that allocate gigabytes and release them faster than the Prometheus
// scrape interval — by the time a human attaches to the pprof port the
// heap is back to normal. A watcher inside the process is the only
// vantage point that reliably sees the spike.
package memwatch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	thresholdFlag = "memwatch-threshold-mb"
	dirFlag       = "memwatch-dir"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   thresholdFlag,
			Usage:  "heap size (MiB) above which pprof heap+goroutine profiles are dumped (0 disables)",
			EnvVar: "MEMWATCH_THRESHOLD_MB",
			Value:  2048,
		},
		cli.StringFlag{
			Name:   dirFlag,
			Usage:  "directory for memwatch profile dumps",
			EnvVar: "MEMWATCH_DIR",
			Value:  "/tmp/memwatch",
		},
	)
}

type Watch struct {
	threshold uint64 // bytes
	cooldown  time.Duration
	keep      int // dump generations to retain
	dir       string
	interval  time.Duration

	readHeap func() uint64
	dump     func(now time.Time) error

	lastDump time.Time
}

func New(c *cli.Context) *Watch {
	if c.Int(thresholdFlag) <= 0 {
		return nil
	}
	w := &Watch{
		threshold: uint64(c.Int(thresholdFlag)) << 20,
		cooldown:  10 * time.Minute,
		keep:      3,
		dir:       c.String(dirFlag),
		interval:  2 * time.Second,
		readHeap: func() uint64 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return m.HeapAlloc
		},
	}
	w.dump = w.writeProfiles
	return w
}

// Serve blocks until ctx is done. Registered as a plain goroutine, not a
// cs.Servable: it must never take the process down.
func (w *Watch) Serve(ctx context.Context) {
	log.WithField("threshold_bytes", w.threshold).Info("starting memwatch")
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			w.maybeDump(now)
		}
	}
}

func (w *Watch) maybeDump(now time.Time) {
	heap := w.readHeap()
	if heap < w.threshold {
		return
	}
	if !w.lastDump.IsZero() && now.Sub(w.lastDump) < w.cooldown {
		return
	}
	w.lastDump = now
	// error level on purpose: the spike itself is the incident being
	// hunted, and this line is the Loki marker that profiles exist.
	e := log.WithFields(log.Fields{
		"heap_bytes": heap,
		"dir":        w.dir,
	})
	if err := w.dump(now); err != nil {
		e.WithError(err).Error("memwatch: heap above threshold, profile dump failed")
		return
	}
	e.Error("memwatch: heap above threshold, profiles dumped")
}

func (w *Watch) writeProfiles(now time.Time) error {
	if err := os.MkdirAll(w.dir, 0755); err != nil {
		return err
	}
	stamp := now.UTC().Format("20060102-150405")
	for _, kind := range []string{"heap", "goroutine"} {
		f, err := os.Create(filepath.Join(w.dir, kind+"-"+stamp+".pb.gz"))
		if err != nil {
			return err
		}
		err = pprof.Lookup(kind).WriteTo(f, 0)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	w.prune()
	return nil
}

// prune keeps the newest `keep` timestamps of each profile kind; the
// stamped names sort chronologically.
func (w *Watch) prune() {
	for _, kind := range []string{"heap", "goroutine"} {
		files, err := filepath.Glob(filepath.Join(w.dir, kind+"-*.pb.gz"))
		if err != nil {
			continue
		}
		sort.Strings(files)
		if len(files) <= w.keep {
			continue
		}
		for _, p := range files[:len(files)-w.keep] {
			_ = os.Remove(p)
		}
	}
}
