//go:build scenario

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

// CPU and contention sampling are process-wide, so only one session may own them.
var debugProfileMutex sync.Mutex

type debugProfiling struct {
	w                 *os.File
	directory         string
	label             string
	startedAt         time.Time
	before            runtime.MemStats
	previousMutexRate int
}

type debugProfileResult struct {
	Label            string    `json:"label"`
	Directory        string    `json:"directory"`
	ProcessID        int       `json:"process_id"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	DurationMS       int64     `json:"duration_ms"`
	AllocatedBytes   uint64    `json:"allocated_bytes"`
	AllocatedObjects uint64    `json:"allocated_objects"`
	LiveBytesBefore  uint64    `json:"live_bytes_before"`
	LiveBytesAfter   uint64    `json:"live_bytes_after"`
	GCCount          uint32    `json:"gc_count"`
	GCPauseNS        uint64    `json:"gc_pause_ns"`
}

func (e *debugSupervisor) startProfiling(label string) (debugProfileResult, error) {
	if !debugDiagnosticLabelPattern.MatchString(label) {
		return debugProfileResult{}, errors.New("profile label requires 1 to 48 lowercase letters, digits, underscores or hyphens")
	}
	if e.profiling != nil || !debugProfileMutex.TryLock() {
		return debugProfileResult{}, errors.New("a scenario profile is already active")
	}
	directory := filepath.Join(e.reportDirectory, "profiles", time.Now().UTC().Format("20060102T150405.000000000")+"-"+label)
	err := os.MkdirAll(directory, 0700)
	if err != nil {
		debugProfileMutex.Unlock()
		return debugProfileResult{}, fmt.Errorf("profileDirectory: %w", err)
	}
	runtime.GC()
	for _, name := range []string{"heap", "block", "mutex"} {
		err = writeDebugRuntimeProfile(directory, name+"-before.pprof", name)
		if err != nil {
			debugProfileMutex.Unlock()
			return debugProfileResult{}, fmt.Errorf("profileBaseline[%s]: %w", name, err)
		}
	}
	w, err := os.OpenFile(filepath.Join(directory, "cpu.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		debugProfileMutex.Unlock()
		return debugProfileResult{}, fmt.Errorf("profileCPUCreate: %w", err)
	}
	profiling := &debugProfiling{w: w, directory: directory, label: label}
	runtime.ReadMemStats(&profiling.before)
	profiling.previousMutexRate = runtime.SetMutexProfileFraction(10)
	// No production owner enables block sampling; restore the disabled rate on stop.
	runtime.SetBlockProfileRate(1_000_000)
	err = pprof.StartCPUProfile(w)
	if err != nil {
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(profiling.previousMutexRate)
		closeErr := w.Close()
		debugProfileMutex.Unlock()
		return debugProfileResult{}, fmt.Errorf("profileCPUStart: %w", errors.Join(err, closeErr))
	}
	profiling.startedAt = time.Now().UTC()
	e.profiling = profiling
	e.logLuaActivity("Profiling " + label + " -> " + directory)
	return debugProfileResult{Label: label, Directory: directory, ProcessID: os.Getpid(), StartedAt: profiling.startedAt}, nil
}

func (e *debugSupervisor) stopProfiling() (debugProfileResult, error) {
	profiling := e.profiling
	if profiling == nil {
		return debugProfileResult{}, errors.New("no scenario profile is active")
	}
	e.profiling = nil
	defer debugProfileMutex.Unlock()
	pprof.StopCPUProfile()
	finishedAt := time.Now().UTC()
	closeErr := profiling.w.Close()
	runtime.SetBlockProfileRate(0)
	runtime.SetMutexProfileFraction(profiling.previousMutexRate)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	result := debugProfileResult{Label: profiling.label, Directory: profiling.directory, ProcessID: os.Getpid(),
		StartedAt: profiling.startedAt, FinishedAt: finishedAt, DurationMS: finishedAt.Sub(profiling.startedAt).Milliseconds(),
		AllocatedBytes: after.TotalAlloc - profiling.before.TotalAlloc, AllocatedObjects: after.Mallocs - profiling.before.Mallocs,
		LiveBytesBefore: profiling.before.HeapAlloc, LiveBytesAfter: after.HeapAlloc,
		GCCount: after.NumGC - profiling.before.NumGC, GCPauseNS: after.PauseTotalNs - profiling.before.PauseTotalNs}
	var profileErr error
	if closeErr != nil {
		profileErr = fmt.Errorf("profileCPUClose: %w", closeErr)
	}
	for _, name := range []string{"heap", "block", "mutex", "goroutine"} {
		err := writeDebugRuntimeProfile(profiling.directory, name+".pprof", name)
		if err != nil {
			profileErr = errors.Join(profileErr, fmt.Errorf("profileWrite[%s]: %w", name, err))
		}
	}
	err := writeDebugJSON(filepath.Join(profiling.directory, "summary.json"), result)
	if err != nil {
		profileErr = errors.Join(profileErr, fmt.Errorf("profileSummary: %w", err))
	}
	e.logLuaActivity(fmt.Sprintf("Profile %s finished in %dms; allocated=%d live=%d -> %d", result.Label, result.DurationMS, result.AllocatedBytes, result.LiveBytesBefore, result.LiveBytesAfter))
	if profileErr != nil {
		return result, fmt.Errorf("profileFinish: %w", profileErr)
	}
	return result, nil
}

func writeDebugRuntimeProfile(directory, name, kind string) error {
	profile := pprof.Lookup(kind)
	if profile == nil {
		return fmt.Errorf("runtime profile %q unavailable", kind)
	}
	w, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("profileCreate: %w", err)
	}
	writeErr := profile.WriteTo(w, 0)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("profileOutput: %w", errors.Join(writeErr, closeErr))
	}
	return nil
}

func (e *debugSupervisor) finishProfiling() {
	e.operationMutex.Lock()
	defer e.operationMutex.Unlock()
	if e.profiling == nil {
		return
	}
	result, err := e.stopProfiling()
	if err != nil {
		e.logFailure("Automatic profile finish", err)
		return
	}
	e.logLuaActivity("Automatic profile saved: " + result.Directory)
}
