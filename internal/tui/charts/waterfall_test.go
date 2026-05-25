package charts

import (
	"strings"
	"testing"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TestRenderWaterfallChartEmptyExplainsMissingTimeline verifies the adapter renders an actionable empty state.
func TestRenderWaterfallChartEmptyExplainsMissingTimeline(t *testing.T) {
	got := RenderWaterfallChart(whatttft.RequestRecord{}, WaterfallOptions{Width: 60, Height: 4}, PlainTheme())
	if !strings.Contains(got, "waterfall unavailable: timeline events missing") {
		t.Fatalf("empty waterfall adapter missing explanation:\n%s", got)
	}
}

// TestWaterfallEmpty verifies a record with no timeline phases renders an unavailable state.
func TestWaterfallEmpty(t *testing.T) {
	got := Waterfall(whatttft.RequestRecord{}, 80)
	if !strings.Contains(got, "(no observed phases)") {
		t.Fatalf("empty waterfall = %q, want no observed phases", got)
	}
}

// TestWaterfallObservedPhases verifies observed phases are labeled and missing phases are omitted.
func TestWaterfallObservedPhases(t *testing.T) {
	record := whatttft.RequestRecord{Timeline: whatttft.Timeline{EventsNS: map[whatttft.EventName]int64{
		whatttft.EventRequestStart:      0,
		whatttft.EventDNSStart:          1_000_000,
		whatttft.EventDNSDone:           3_000_000,
		whatttft.EventGotConn:           5_000_000,
		whatttft.EventWroteRequest:      7_000_000,
		whatttft.EventFirstResponseByte: 17_000_000,
		whatttft.EventFirstOutputDelta:  30_000_000,
		whatttft.EventLastOutputDelta:   90_000_000,
	}}}

	got := Waterfall(record, 60)
	for _, want := range []string{"dns", "connection acquire", "request write", "server wait to first byte", "stream protocol to first output", "visible-generation deltas"} {
		if !strings.Contains(got, want) {
			t.Fatalf("waterfall missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "tls") {
		t.Fatalf("waterfall should omit missing tls phase:\n%s", got)
	}
}

// TestWaterfallTinyWidth verifies small widths still render labels and values.
func TestWaterfallTinyWidth(t *testing.T) {
	record := whatttft.RequestRecord{Timeline: whatttft.Timeline{EventsNS: map[whatttft.EventName]int64{
		whatttft.EventRequestStart: 0,
		whatttft.EventGotConn:      0,
	}}}
	got := Waterfall(record, 1)
	if !strings.Contains(got, "connection acquire") || !strings.Contains(got, "0.0") {
		t.Fatalf("tiny waterfall unexpected:\n%s", got)
	}
}
