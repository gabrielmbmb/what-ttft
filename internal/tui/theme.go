package tui

import (
	"os"

	"charm.land/lipgloss/v2"
	"github.com/gabrielmbmb/what-ttft/internal/tui/charts"
)

type themeRole int

const (
	roleAccent themeRole = iota
	roleGood
	roleWarn
	roleBad
	roleMuted
	roleBorder
	roleChartTTFT
	roleChartE2E
	roleChartTPS
	roleChartWaterfall
	roleBackground
)

type tuiTheme struct {
	noColor        bool
	Accent         lipgloss.Style
	Good           lipgloss.Style
	Warn           lipgloss.Style
	Bad            lipgloss.Style
	Muted          lipgloss.Style
	Border         lipgloss.Style
	ChartTTFT      lipgloss.Style
	ChartE2E       lipgloss.Style
	ChartTPS       lipgloss.Style
	ChartWaterfall lipgloss.Style
	Background     lipgloss.Style
}

func defaultTheme() tuiTheme {
	_, noColor := os.LookupEnv("NO_COLOR")
	return newTheme(noColor)
}

func newTheme(noColor bool) tuiTheme {
	if noColor {
		return tuiTheme{
			noColor:        true,
			Accent:         lipgloss.NewStyle(),
			Good:           lipgloss.NewStyle(),
			Warn:           lipgloss.NewStyle(),
			Bad:            lipgloss.NewStyle(),
			Muted:          lipgloss.NewStyle(),
			Border:         lipgloss.NewStyle(),
			ChartTTFT:      lipgloss.NewStyle(),
			ChartE2E:       lipgloss.NewStyle(),
			ChartTPS:       lipgloss.NewStyle(),
			ChartWaterfall: lipgloss.NewStyle(),
			Background:     lipgloss.NewStyle(),
		}
	}

	return tuiTheme{
		Accent:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		Good:           lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		Warn:           lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
		Bad:            lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		Muted:          lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		Border:         lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		ChartTTFT:      lipgloss.NewStyle().Foreground(lipgloss.Color("81")),
		ChartE2E:       lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
		ChartTPS:       lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		ChartWaterfall: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		Background:     lipgloss.NewStyle(),
	}
}

func (theme tuiTheme) style(role themeRole) lipgloss.Style {
	switch role {
	case roleAccent:
		return theme.Accent
	case roleGood:
		return theme.Good
	case roleWarn:
		return theme.Warn
	case roleBad:
		return theme.Bad
	case roleMuted:
		return theme.Muted
	case roleBorder:
		return theme.Border
	case roleChartTTFT:
		return theme.ChartTTFT
	case roleChartE2E:
		return theme.ChartE2E
	case roleChartTPS:
		return theme.ChartTPS
	case roleChartWaterfall:
		return theme.ChartWaterfall
	default:
		return theme.Background
	}
}

func (theme tuiTheme) render(role themeRole, text string) string {
	return theme.style(role).Render(text)
}

func (theme tuiTheme) chartTheme(role themeRole) charts.Theme {
	return charts.Theme{
		Axis:            theme.Border,
		Label:           theme.Muted,
		Series:          theme.style(role),
		SecondarySeries: theme.style(role),
		Muted:           theme.Muted,
	}
}
