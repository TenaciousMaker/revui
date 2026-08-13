package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestWheelInputFilterDropsBufferedFloodButKeepsLiveGesture(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	filter := &wheelInputFilter{now: func() time.Time { return now }}
	wheel := tea.MouseWheelMsg{X: 100, Y: 10, Button: tea.MouseWheelDown}

	accepted := 0
	for range 100 {
		if filter.apply(nil, wheel) != nil {
			accepted++
		}
		now = now.Add(10 * time.Microsecond)
	}
	if accepted > wheelFloodThreshold {
		t.Fatalf("buffered flood accepted %d events, want <=%d", accepted, wheelFloodThreshold)
	}

	now = now.Add(wheelFloodRecovery + time.Millisecond)
	if filter.apply(nil, wheel) == nil {
		t.Fatal("wheel event was not accepted after the recovery interval")
	}

	live := &wheelInputFilter{now: func() time.Time { return now }}
	for event := 0; event < 100; event++ {
		if live.apply(nil, wheel) == nil {
			t.Fatalf("live gesture event %d was dropped", event)
		}
		now = now.Add(5 * time.Millisecond)
	}
}

func TestWheelInputFilterLetsOppositeDirectionInterruptFlood(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	filter := &wheelInputFilter{now: func() time.Time { return now }}
	down := tea.MouseWheelMsg{X: 100, Y: 10, Button: tea.MouseWheelDown}
	for range wheelFloodThreshold + 2 {
		_ = filter.apply(nil, down)
		now = now.Add(10 * time.Microsecond)
	}
	if filter.apply(nil, tea.MouseWheelMsg{X: 100, Y: 10, Button: tea.MouseWheelUp}) == nil {
		t.Fatal("opposite wheel direction did not interrupt a buffered flood")
	}
}
