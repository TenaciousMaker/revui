package ui

import (
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TenaciousMaker/revui/internal/diff"
	"github.com/TenaciousMaker/revui/internal/gitrepo"
)

type wheelProgramProbe struct {
	scroll      atomic.Int64
	wheelEvents atomic.Int64
	views       atomic.Int64
	scheduled   atomic.Bool
}

type observedWheelProgramModel struct {
	model Model
	probe *wheelProgramProbe
}

type bufferedTerminalInput struct {
	mu     sync.Mutex
	ready  *sync.Cond
	data   []byte
	closed bool
}

func newBufferedTerminalInput() *bufferedTerminalInput {
	input := &bufferedTerminalInput{}
	input.ready = sync.NewCond(&input.mu)
	return input
}

func (i *bufferedTerminalInput) Read(buffer []byte) (int, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for len(i.data) == 0 && !i.closed {
		i.ready.Wait()
	}
	if len(i.data) == 0 {
		return 0, io.EOF
	}
	n := copy(buffer, i.data)
	i.data = i.data[n:]
	return n, nil
}

func (i *bufferedTerminalInput) Write(data []byte) (int, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return 0, io.ErrClosedPipe
	}
	i.data = append(i.data, data...)
	i.ready.Signal()
	return len(data), nil
}

func (i *bufferedTerminalInput) Close() error {
	i.mu.Lock()
	i.closed = true
	i.ready.Broadcast()
	i.mu.Unlock()
	return nil
}

func (m *observedWheelProgramModel) Init() tea.Cmd { return nil }

func (m *observedWheelProgramModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.model.Update(msg)
	m.model = updated.(Model)
	m.probe.scroll.Store(int64(m.model.lineScroll)*1_000_000 + int64(m.model.lineWrapOffset))
	m.probe.scheduled.Store(m.model.wheelScheduled)
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		m.probe.wheelEvents.Add(1)
	}
	return m, cmd
}

func (m *observedWheelProgramModel) View() tea.View {
	m.probe.views.Add(1)
	return m.model.View()
}

func TestRawMouseWheelBurstStopsPromptlyAfterInputEnds(t *testing.T) {
	var lines []diff.Line
	text := strings.Repeat("text-heavy source content with identifiers and punctuation; ", 20)
	for line := 1; line <= 2000; line++ {
		lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: line})
	}
	repo := &gitrepo.Repository{
		Root: t.TempDir(), Branch: "feature", Base: "main", ReviewPath: filepath.Join(t.TempDir(), "review.json"),
		Files: []diff.File{{Path: "Large.java", Lines: lines}},
	}
	model, err := newTestModel(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	model.width, model.height, model.focus = 140, 40, focusDiff

	input := newBufferedTerminalInput()
	probe := &wheelProgramProbe{}
	program := tea.NewProgram(
		&observedWheelProgramModel{model: model, probe: probe},
		tea.WithInput(input),
		tea.WithOutput(io.Discard),
		tea.WithWindowSize(140, 40),
		tea.WithoutSignals(),
		tea.WithFilter(NewInputFilter()),
	)
	done := make(chan error, 1)
	go func() {
		_, runErr := program.Run()
		done <- runErr
	}()
	t.Cleanup(func() {
		program.Kill()
		_ = input.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})

	deadline := time.Now().Add(time.Second)
	for probe.views.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if probe.views.Load() == 0 {
		t.Fatal("Bubble Tea program did not start")
	}

	const wheelDown = "\x1b[<65;101;11M"
	payload := strings.Repeat(wheelDown, 20_000)
	if _, err := io.WriteString(input, payload); err != nil {
		t.Fatal(err)
	}

	settleDeadline := time.Now().Add(150 * time.Millisecond)
	for (probe.wheelEvents.Load() == 0 || probe.scheduled.Load()) && time.Now().Before(settleDeadline) {
		time.Sleep(time.Millisecond)
	}
	if probe.wheelEvents.Load() == 0 || probe.scheduled.Load() {
		t.Fatalf(
			"wheel input did not settle within 150ms: position=%d wheel-events=%d views=%d scheduled=%v",
			probe.scroll.Load(),
			probe.wheelEvents.Load(),
			probe.views.Load(),
			probe.scheduled.Load(),
		)
	}
	positionAfterSettle := probe.scroll.Load()
	time.Sleep(100 * time.Millisecond)
	positionAfterDrain := probe.scroll.Load()
	if positionAfterDrain != positionAfterSettle {
		t.Fatalf(
			"scroll continued after settling: settled=%d after-100ms=%d wheel-events=%d views=%d",
			positionAfterSettle,
			positionAfterDrain,
			probe.wheelEvents.Load(),
			probe.views.Load(),
		)
	}
}
