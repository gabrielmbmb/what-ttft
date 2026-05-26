package charts

import "charm.land/lipgloss/v2"

// Theme contains semantic Lip Gloss styles used by chart adapters.
type Theme struct {
	// Axis styles chart axes and non-data guide marks; zero value renders without color.
	Axis lipgloss.Style

	// Label styles chart labels, titles, and explanatory text; zero value renders without color.
	Label lipgloss.Style

	// Series styles primary data marks such as lines or bars; zero value renders without color.
	Series lipgloss.Style

	// SecondarySeries styles comparison or supporting data marks; zero value renders without color.
	SecondarySeries lipgloss.Style

	// Palette optionally provides per-series styles for multi-series comparison charts; nil means Series, SecondarySeries, and Muted are reused.
	Palette []lipgloss.Style

	// Muted styles empty-state and low-emphasis text; zero value renders without color.
	Muted lipgloss.Style
}

// PlainTheme returns an uncolored chart theme suitable for tests and no-color terminals.
func PlainTheme() Theme {
	return Theme{}
}
