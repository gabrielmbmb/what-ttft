package tui

import "charm.land/lipgloss/v2"

type styles struct {
	title   lipgloss.Style
	muted   lipgloss.Style
	status  lipgloss.Style
	error   lipgloss.Style
	section lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		title:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		status:  lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		error:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		section: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
	}
}
