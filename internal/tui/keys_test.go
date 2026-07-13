package tui

import (
	"slices"
	"testing"
)

// TestDefaultKeyMapIncludesModelMetrics verifies the focused model metrics view has a dedicated shortcut.
func TestDefaultKeyMapIncludesModelMetrics(t *testing.T) {
	keys := defaultKeyMap()
	if !slices.Contains(keys.Metrics.Keys(), "m") {
		t.Fatalf("model metrics keys = %v, want m", keys.Metrics.Keys())
	}
}
