// Package metrics collects host CPU and memory statistics and streams them
// to a caller-supplied callback on a fixed interval.
package metrics

import (
	"context"
	"log"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

const collectInterval = 5 * time.Second

// Snapshot holds a single host resource reading.
type Snapshot struct {
	CPUPercent float64 // 0–100, averaged across all logical cores
	MemUsedMB  uint64  // resident memory in MiB
	MemTotalMB uint64  // total physical RAM in MiB
}

// CollectFunc is called on each successful collection.
type CollectFunc func(Snapshot)

// Collector samples CPU and memory at a fixed interval.
// Call Run to start it; it blocks until ctx is cancelled.
func Run(ctx context.Context, interval time.Duration, fn CollectFunc) {
	if interval <= 0 {
		interval = collectInterval
	}

	// Do an initial collection immediately so the mobile app gets data right
	// away instead of waiting for the first ticker tick.
	if s, ok := collect(); ok {
		fn(s)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s, ok := collect(); ok {
				fn(s)
			}
		}
	}
}

// collect gathers one CPU + memory sample. Returns false on error.
func collect() (Snapshot, bool) {
	// cpu.Percent with a 500 ms window gives a real measurement without
	// blocking for a full interval. perCPU=false returns a single aggregate.
	percents, err := cpu.Percent(500*time.Millisecond, false)
	if err != nil || len(percents) == 0 {
		log.Printf("[metrics] cpu.Percent: %v", err)
		return Snapshot{}, false
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		log.Printf("[metrics] mem.VirtualMemory: %v", err)
		return Snapshot{}, false
	}

	return Snapshot{
		CPUPercent: percents[0],
		MemUsedMB:  vm.Used / (1024 * 1024),
		MemTotalMB: vm.Total / (1024 * 1024),
	}, true
}
