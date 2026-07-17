package ui

import (
	"os"

	"charm.land/lipgloss/v2"
)

const (
	panelBackground       = "#161B22"
	addedLineBackground   = "#1F2A24"
	deletedLineBackground = "#51252B"
	selectedRowBackground = "#1D58B3"
)

type theme struct {
	color                                                bool
	canvas, panel, raised, selected, border, text, muted lipgloss.Style
	focus, cursor                                        lipgloss.Style
	added, deleted, addedText, deletedText, modifiedText lipgloss.Style
	renamedText, liveText, hunk                          lipgloss.Style
}

func newTheme() theme {
	return newThemeWithColor(os.Getenv("NO_COLOR") == "")
}

func newThemeWithColor(colorEnabled bool) theme {
	if !colorEnabled {
		plain := lipgloss.NewStyle()
		return theme{
			color:  false,
			canvas: plain, panel: plain, raised: plain, selected: plain.Reverse(true).Bold(true), border: plain,
			text: plain, muted: plain.Faint(true), focus: plain.Bold(true),
			cursor: plain.Reverse(true), added: plain, deleted: plain,
			addedText: plain.Bold(true), deletedText: plain.Bold(true), modifiedText: plain, renamedText: plain, liveText: plain.Bold(true), hunk: plain.Bold(true),
		}
	}
	canvas := lipgloss.Color("#0D1117")
	panel := lipgloss.Color(panelBackground)
	raised := lipgloss.Color("#1F2630")
	border := lipgloss.Color("#30363D")
	text := lipgloss.Color("#C9D1D9")
	muted := lipgloss.Color("#8B949E")
	focus := lipgloss.Color("#58A6FF")
	return theme{
		color:        true,
		canvas:       lipgloss.NewStyle().Background(canvas).Foreground(text),
		panel:        lipgloss.NewStyle().Background(panel).Foreground(text),
		raised:       lipgloss.NewStyle().Background(raised).Foreground(text),
		selected:     lipgloss.NewStyle().Background(lipgloss.Color(selectedRowBackground)).Foreground(lipgloss.Color("#A5D6FF")).Bold(true),
		border:       lipgloss.NewStyle().Foreground(border),
		text:         lipgloss.NewStyle().Foreground(text),
		muted:        lipgloss.NewStyle().Foreground(muted),
		focus:        lipgloss.NewStyle().Foreground(focus).Bold(true),
		cursor:       lipgloss.NewStyle().Foreground(canvas).Background(text),
		added:        lipgloss.NewStyle().Background(lipgloss.Color(addedLineBackground)).Foreground(text),
		deleted:      lipgloss.NewStyle().Background(lipgloss.Color(deletedLineBackground)).Foreground(text),
		addedText:    lipgloss.NewStyle().Foreground(lipgloss.Color("#56D364")),
		deletedText:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FF7B72")),
		modifiedText: lipgloss.NewStyle().Foreground(lipgloss.Color("#79C0FF")),
		renamedText:  lipgloss.NewStyle().Foreground(lipgloss.Color("#D2A8FF")),
		liveText:     lipgloss.NewStyle().Foreground(lipgloss.Color("#D2A8FF")),
		hunk:         lipgloss.NewStyle().Background(lipgloss.Color("#172B4D")).Foreground(lipgloss.Color("#79C0FF")),
	}
}
