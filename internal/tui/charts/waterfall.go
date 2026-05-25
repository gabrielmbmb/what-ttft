package charts

import (
	"fmt"
	"strings"
	"time"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// WaterfallOptions configures RenderWaterfallChart output.
type WaterfallOptions struct {
	// Width is the target rendered width in terminal cells; values less than one render an empty string.
	Width int

	// Height is the target rendered height in terminal rows; values less than one render an empty string.
	Height int

	// Title is the non-secret chart title; empty defaults to "waterfall ms".
	Title string

	// EmptyLabel is the no-data explanation; empty defaults to "waterfall unavailable: timeline events missing".
	EmptyLabel string
}

// RenderWaterfallChart renders observed request timeline phases with project-specific latency labels.
func RenderWaterfallChart(record whatttft.RequestRecord, opts WaterfallOptions, _ Theme) string {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ""
	}
	title := opts.Title
	if strings.TrimSpace(title) == "" {
		title = "waterfall ms"
	}
	if len(waterfallPhases(record.Timeline)) == 0 {
		empty := opts.EmptyLabel
		if strings.TrimSpace(empty) == "" {
			empty = "waterfall unavailable: timeline events missing"
		}
		return fitChartText(strings.Join([]string{title, empty}, "\n"), opts.Width, opts.Height)
	}
	return fitChartText(strings.Join([]string{title, Waterfall(record, opts.Width)}, "\n"), opts.Width, opts.Height)
}

// Waterfall renders observed request timeline phases in milliseconds, omitting phases with missing endpoints.
func Waterfall(record whatttft.RequestRecord, width int) string {
	phases := waterfallPhases(record.Timeline)
	if len(phases) == 0 {
		return "waterfall ms\n(no observed phases)"
	}

	maxDuration := int64(0)
	for _, phase := range phases {
		if phase.DurationNS > maxDuration {
			maxDuration = phase.DurationNS
		}
	}
	barWidth := width - 34
	if barWidth < 1 {
		barWidth = 1
	}

	var builder strings.Builder
	builder.WriteString("waterfall ms\n")
	for index, phase := range phases {
		ms := float64(phase.DurationNS) / float64(time.Millisecond)
		bar := strings.Repeat("█", scaledInt64Width(phase.DurationNS, maxDuration, barWidth))
		fmt.Fprintf(&builder, "%-31s %8.1f %s", phase.Label, ms, bar)
		if index != len(phases)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}

type waterfallPhase struct {
	Label      string
	DurationNS int64
}

func waterfallPhases(timeline whatttft.Timeline) []waterfallPhase {
	definitions := []struct {
		label string
		start whatttft.EventName
		end   whatttft.EventName
	}{
		{label: "dns", start: whatttft.EventDNSStart, end: whatttft.EventDNSDone},
		{label: "tcp", start: whatttft.EventConnectStart, end: whatttft.EventConnectDone},
		{label: "tls", start: whatttft.EventTLSStart, end: whatttft.EventTLSDone},
		{label: "connection acquire", start: whatttft.EventRequestStart, end: whatttft.EventGotConn},
		{label: "request write", start: whatttft.EventGotConn, end: whatttft.EventWroteRequest},
		{label: "server wait to first byte", start: whatttft.EventWroteRequest, end: whatttft.EventFirstResponseByte},
		{label: "first byte to first SSE", start: whatttft.EventFirstResponseByte, end: whatttft.EventFirstSSEEvent},
		{label: "stream protocol to first output", start: whatttft.EventFirstSSEEvent, end: whatttft.EventFirstOutputDelta},
		{label: "visible-generation deltas", start: whatttft.EventFirstOutputDelta, end: whatttft.EventLastOutputDelta},
	}

	phases := make([]waterfallPhase, 0, len(definitions))
	for _, definition := range definitions {
		duration, ok := phaseDuration(timeline, definition.start, definition.end)
		if !ok && definition.label == "stream protocol to first output" {
			duration, ok = phaseDuration(timeline, whatttft.EventFirstResponseByte, whatttft.EventFirstOutputDelta)
		}
		if !ok {
			continue
		}
		phases = append(phases, waterfallPhase{Label: definition.label, DurationNS: duration})
	}

	return phases
}

func phaseDuration(timeline whatttft.Timeline, start whatttft.EventName, end whatttft.EventName) (int64, bool) {
	if timeline.EventsNS == nil {
		return 0, false
	}
	startNS, ok := timeline.EventsNS[start]
	if !ok {
		return 0, false
	}
	endNS, ok := timeline.EventsNS[end]
	if !ok {
		return 0, false
	}
	return endNS - startNS, true
}

func scaledInt64Width(value int64, maxValue int64, width int) int {
	if value <= 0 || maxValue <= 0 || width <= 0 {
		return 0
	}
	if value == maxValue {
		return width
	}
	result := int(float64(value) / float64(maxValue) * float64(width))
	if result < 1 {
		return 1
	}
	return result
}
