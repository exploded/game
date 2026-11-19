package jobs

import (
	"testing"
	"time"
)

func TestRunRecoversAPanic(t *testing.T) {
	ran := 0
	for range 3 {
		Run("test", func() {
			ran++
			panic("boom")
		})
	}
	if ran != 3 {
		t.Fatalf("ran %d times, want 3: a panic stopped the loop", ran)
	}
	if Run("test", func() {}) {
		t.Error("a job that didn't panic reported a panic")
	}
}

func TestGoRecoversAndWaitWaits(t *testing.T) {
	release := make(chan struct{})
	Go("panics", func() { panic("boom") })
	Go("slow", func() { <-release })
	if Wait(50 * time.Millisecond) {
		t.Fatal("Wait returned true while a job was still running")
	}
	close(release)
	if !Wait(2 * time.Second) {
		t.Fatal("Wait timed out after every job returned")
	}
	if !Wait(0) {
		t.Fatal("Wait with nothing running didn't return true")
	}
}

func TestSlot(t *testing.T) {
	var s Slot
	if s.Due("") {
		t.Error("an empty slot is due")
	}
	if !s.Due("2026-11-19 04") {
		t.Fatal("a new slot isn't due")
	}
	s.Done("2026-11-19 04")
	if s.Due("2026-11-19 04") {
		t.Error("a slot already run is due again")
	}
	if !s.Due("2026-11-19 08") {
		t.Error("the next slot isn't due")
	}
}
