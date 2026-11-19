// Package jobs runs the app's background work safely: a panic in one job is
// logged with its stack instead of killing the process or stopping the loop
// that started it, and shutdown can wait for running jobs to finish.
package jobs

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// running counts goroutines started by Go, so Wait can tell when they've
// all returned. idle is closed when the count drops to zero.
var running struct {
	mu   sync.Mutex
	n    int
	idle chan struct{}
}

// Go runs fn in a goroutine that recovers a panic (Recover) and is counted
// for Wait. name says what the job is in the log.
func Go(name string, fn func()) {
	running.mu.Lock()
	if running.n == 0 {
		running.idle = make(chan struct{})
	}
	running.n++
	running.mu.Unlock()
	go func() {
		defer func() {
			running.mu.Lock()
			running.n--
			if running.n == 0 {
				close(running.idle)
			}
			running.mu.Unlock()
		}()
		defer Recover(name)
		fn()
	}()
}

// Run calls fn, recovering a panic (Recover), and reports whether it
// panicked. A loop calls each step through it so one bad run doesn't end
// the loop.
func Run(name string, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(name, r)
			panicked = true
		}
	}()
	fn()
	return false
}

// Recover logs a panic with its stack and stops it going further. It only
// works deferred directly: defer jobs.Recover("name").
func Recover(name string) {
	if r := recover(); r != nil {
		logPanic(name, r)
	}
}

func logPanic(name string, r any) {
	slog.Error("background job panicked", "job", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
}

// Wait waits up to d for every goroutine started by Go to return, and
// reports whether they all did.
func Wait(d time.Duration) bool {
	running.mu.Lock()
	if running.n == 0 {
		running.mu.Unlock()
		return true
	}
	idle := running.idle
	running.mu.Unlock()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-idle:
		return true
	case <-t.C:
		return false
	}
}

// Slot remembers the last time slot a job ran in, so a job meant to run once
// in a window (on the hour, at 23:00) still runs once when the tick that
// opens the window is late or skipped. A slot is any string naming the
// window, such as "2026-11-19 04".
type Slot struct {
	mu   sync.Mutex
	last string
}

// Due reports whether the job hasn't run in slot yet. An empty slot means
// "not in a window" and is never due.
func (s *Slot) Due(slot string) bool {
	if slot == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last != slot
}

// Done records that the job ran (or started) in slot.
func (s *Slot) Done(slot string) {
	s.mu.Lock()
	s.last = slot
	s.mu.Unlock()
}
