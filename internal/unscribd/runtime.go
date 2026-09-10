package unscribd

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

func effectiveConcurrency(requested int) int {
	if requested < 1 {
		requested = 1
	}
	if requested > 32 {
		requested = 32
	}
	if value := os.Getenv("UNSCRIBD_MAX_CONCURRENCY"); value != "" {
		if limit, err := strconv.Atoi(value); err == nil && limit > 0 && requested > limit {
			requested = limit
		}
	}
	// A single logical processor is common on small hosted instances. Keep
	// network parallelism useful while preventing image composition from
	// creating an excessive CPU runnable set.
	if runtime.GOMAXPROCS(0) == 1 && requested > 4 {
		requested = 4
	}
	if quota := cgroupCPUQuota(); quota > 0 {
		if quota < 1 && requested > 2 {
			requested = 2
		} else if quota < 2 && requested > 4 {
			requested = 4
		}
	}
	if memory := cgroupMemoryLimit(); memory > 0 && memory < 768<<20 && requested > 4 {
		requested = 4
	}
	return requested
}

func cgroupCPUQuota() float64 {
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(data))
	if len(parts) != 2 || parts[0] == "max" {
		return 0
	}
	quota, err1 := strconv.ParseFloat(parts[0], 64)
	period, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || period <= 0 {
		return 0
	}
	return quota / period
}

func cgroupMemoryLimit() int64 {
	data, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil || strings.TrimSpace(string(data)) == "max" {
		return 0
	}
	limit, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return limit
}
