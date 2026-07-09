package ui

import "charm.land/lipgloss/v2"

type theme struct {
	canvas, panel, raised, border, text, muted, focus, comment lipgloss.Style
	added, deleted, addedText, deletedText, hunk               lipgloss.Style
}

func newTheme() theme {
	canvas := lipgloss.Color("#0D1117")
	panel := lipgloss.Color("#161B22")
	raised := lipgloss.Color("#1F2630")
	border := lipgloss.Color("#30363D")
	text := lipgloss.Color("#C9D1D9")
	muted := lipgloss.Color("#8B949E")
	focus := lipgloss.Color("#58A6FF")
	comment := lipgloss.Color("#D2A8FF")
	return theme{
		canvas:      lipgloss.NewStyle().Background(canvas).Foreground(text),
		panel:       lipgloss.NewStyle().Background(panel).Foreground(text),
		raised:      lipgloss.NewStyle().Background(raised).Foreground(text),
		border:      lipgloss.NewStyle().Foreground(border),
		text:        lipgloss.NewStyle().Foreground(text),
		muted:       lipgloss.NewStyle().Foreground(muted),
		focus:       lipgloss.NewStyle().Foreground(focus).Bold(true),
		comment:     lipgloss.NewStyle().Foreground(comment).Bold(true),
		added:       lipgloss.NewStyle().Background(lipgloss.Color("#132D21")).Foreground(text),
		deleted:     lipgloss.NewStyle().Background(lipgloss.Color("#3B2023")).Foreground(text),
		addedText:   lipgloss.NewStyle().Foreground(lipgloss.Color("#56D364")),
		deletedText: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF7B72")),
		hunk:        lipgloss.NewStyle().Background(lipgloss.Color("#172B4D")).Foreground(lipgloss.Color("#79C0FF")),
	}
}
