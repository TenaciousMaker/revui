package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	wheelFloodGap       = 250 * time.Microsecond
	wheelFloodThreshold = 16
	wheelFloodRecovery  = 75 * time.Millisecond
)

type wheelInputFilter struct {
	now      func() time.Time
	lastAt   time.Time
	button   tea.MouseButton
	rapid    int
	dropping bool
}

// NewInputFilter drops a buffered flood of stale terminal wheel events before
// Bubble Tea calls Update and View for each one. Normal wheel gestures retain
// their original events; only an implausibly fast processed burst is shed.
func NewInputFilter() func(tea.Model, tea.Msg) tea.Msg {
	filter := &wheelInputFilter{now: time.Now}
	return filter.apply
}

func (f *wheelInputFilter) apply(_ tea.Model, msg tea.Msg) tea.Msg {
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.MouseClickMsg:
			f.reset()
		}
		return msg
	}
	if wheel.Button != tea.MouseWheelUp && wheel.Button != tea.MouseWheelDown {
		return msg
	}

	now := f.now()
	gap := now.Sub(f.lastAt)
	if f.lastAt.IsZero() || gap < 0 || gap > wheelFloodRecovery || wheel.Button != f.button {
		f.rapid = 0
		f.dropping = false
	} else if gap <= wheelFloodGap {
		f.rapid++
	} else if !f.dropping {
		f.rapid = 0
	}
	f.lastAt = now
	f.button = wheel.Button

	if f.dropping {
		return nil
	}
	if f.rapid >= wheelFloodThreshold {
		f.dropping = true
		return nil
	}
	return msg
}

func (f *wheelInputFilter) reset() {
	f.lastAt = time.Time{}
	f.button = tea.MouseNone
	f.rapid = 0
	f.dropping = false
}
