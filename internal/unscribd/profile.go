package unscribd

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"time"
)

// Profiling is opt-in to avoid logging during normal downloads.
type profiler struct {
	enabled bool
	start   time.Time
	peak    uint64
}

func newProfiler() profiler {
	return profiler{enabled: os.Getenv("UNSCRIBD_PROFILE") != "", start: time.Now()}
}

func (p *profiler) stage(name string, started time.Time) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if stats.HeapAlloc > p.peak {
		p.peak = stats.HeapAlloc
	}
	if p.enabled {
		log.Printf("unscribd profile %-18s %s heap=%s", name, time.Since(started).Round(time.Millisecond), formatBytes(stats.HeapAlloc))
	}
}

func (p *profiler) total() {
	if p.enabled {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		if stats.HeapAlloc > p.peak {
			p.peak = stats.HeapAlloc
		}
		log.Printf("unscribd profile %-18s %s heap=%s peak=%s sys=%s", "total", time.Since(p.start).Round(time.Millisecond), formatBytes(stats.HeapAlloc), formatBytes(p.peak), formatBytes(stats.Sys))
	}
}

func formatBytes(value uint64) string {
	if value < 1024*1024 {
		return strconv.FormatUint(value/1024, 10) + " KiB"
	}
	return strconv.FormatUint(value/(1024*1024), 10) + " MiB"
}
