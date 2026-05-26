package tui

import (
	"sort"
	"strings"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const (
	requestPhaseMeasured = "measured"
	requestPhaseWarmup   = "warmup"

	requestOutcomeOK    = "ok"
	requestOutcomeError = "error"

	requestCacheHit     = "hit"
	requestCacheMiss    = "miss"
	requestCacheUnknown = "unknown"

	requestConnNew     = "new"
	requestConnReused  = "reused"
	requestConnUnknown = "unknown"

	requestOutputDisabled  = "disabled"
	requestOutputEmpty     = "empty"
	requestOutputAvailable = "available"
	requestOutputTruncated = "truncated"
)

type requestSort string

const (
	requestSortCompletionOrder requestSort = "completion-order"
	requestSortSlowestTTFT     requestSort = "slowest-ttft"
	requestSortSlowestE2E      requestSort = "slowest-e2e"
	requestSortSlowestStream   requestSort = "slowest-stream"
	requestSortHighestTPS      requestSort = "highest-tps"
	requestSortLowestTPS       requestSort = "lowest-tps"
	requestSortErrorsFirst     requestSort = "errors-first"
	requestSortTargetOrder     requestSort = "target-order"
)

type requestRow struct {
	Ordinal          int
	RequestID        string
	Attempt          int
	Phase            string
	TargetID         string
	TargetName       string
	TargetOrdinal    int
	Provider         string
	ProviderAPI      string
	Model            string
	ServiceTier      string
	Outcome          string
	HTTPStatus       string
	ErrorCategory    string
	FinishReason     string
	TTFTMS           *float64
	E2EMS            *float64
	StreamTotalMS    *float64
	TTFBMS           *float64
	ProviderMS       *float64
	E2EOutputTPS     *float64
	GenerationTPS    *float64
	PromptTokens     *int
	CompletionTokens *int
	TotalTokens      *int
	CacheState       string
	CachedTokens     *int
	Conn             string
	Protocol         string
	OutputState      string
}

func (s liveStore) requestRows() []requestRow {
	return buildRequestRows(s, s.completedRecords())
}

func buildRequestRows(store liveStore, records []whatttft.RequestRecord) []requestRow {
	targetOrdinals := store.targetOrdinalMap()
	rows := make([]requestRow, 0, len(records))
	for index, record := range records {
		rows = append(rows, requestRowFromRecord(store, targetOrdinals, index, record))
	}
	return rows
}

func (s liveStore) targetOrdinalMap() map[string]int {
	ordinals := make(map[string]int, len(s.targetOrderIDs))
	for index, targetID := range s.targetOrderIDs {
		ordinals[targetID] = index
	}
	return ordinals
}

func requestRowFromRecord(store liveStore, targetOrdinals map[string]int, ordinal int, record whatttft.RequestRecord) requestRow {
	target := store.targets[record.TargetID]
	targetOrdinal := len(targetOrdinals)
	if value, ok := targetOrdinals[record.TargetID]; ok {
		targetOrdinal = value
	}

	return requestRow{
		Ordinal:          ordinal,
		RequestID:        safeInline(record.RequestID),
		Attempt:          record.Attempt,
		Phase:            requestRowPhase(record),
		TargetID:         safeInline(record.TargetID),
		TargetName:       safeInline(firstNonEmpty(record.TargetName, targetName(target))),
		TargetOrdinal:    targetOrdinal,
		Provider:         safeInline(firstNonEmpty(record.Provider, targetProvider(target), store.provider)),
		ProviderAPI:      safeInline(firstNonEmpty(targetProviderAPI(target), store.providerAPI)),
		Model:            safeInline(firstNonEmpty(record.Model, targetModel(target), store.model)),
		ServiceTier:      safeInline(requestRowServiceTier(record, target)),
		Outcome:          requestRowOutcome(record),
		HTTPStatus:       requestRowHTTPStatus(record),
		ErrorCategory:    safeInline(requestRowErrorCategory(record)),
		FinishReason:     requestRowFinishReason(record),
		TTFTMS:           copyFloat64Pointer(record.Derived.TTFTDeltaMS),
		E2EMS:            copyFloat64Pointer(record.Derived.E2EDeltaMS),
		StreamTotalMS:    copyFloat64Pointer(record.Derived.StreamTotalMS),
		TTFBMS:           copyFloat64Pointer(record.Derived.HTTPTTFBMS),
		ProviderMS:       copyFloat64Pointer(record.HTTP.ProviderProcessingMS),
		E2EOutputTPS:     copyFloat64Pointer(record.Derived.E2EOutputTPS),
		GenerationTPS:    copyFloat64Pointer(record.Derived.GenerationDeltaOutputTPS),
		PromptTokens:     copyIntPointer(record.PromptTokens),
		CompletionTokens: copyIntPointer(record.CompletionTokens),
		TotalTokens:      copyIntPointer(record.TotalTokens),
		CacheState:       requestRowCacheState(record),
		CachedTokens:     requestRowCachedTokens(record),
		Conn:             requestRowConn(record),
		Protocol:         safeInline(firstNonEmpty(record.HTTP.Protocol, "-")),
		OutputState:      requestRowOutputState(record),
	}
}

func requestRowIndex(rows []requestRow, requestID string) int {
	for index, row := range rows {
		if row.RequestID == requestID {
			return index
		}
	}
	return -1
}

func sortRequestRows(rows []requestRow, order requestSort) []requestRow {
	sorted := append([]requestRow(nil), rows...)
	sort.SliceStable(sorted, func(i int, j int) bool {
		left := sorted[i]
		right := sorted[j]
		switch order {
		case requestSortTargetOrder:
			if left.TargetOrdinal != right.TargetOrdinal {
				return left.TargetOrdinal < right.TargetOrdinal
			}
		case requestSortErrorsFirst:
			if left.Outcome != right.Outcome {
				return left.Outcome == requestOutcomeError
			}
		}
		return requestRowCompletionLess(left, right)
	})
	return sorted
}

func requestRowCompletionLess(left requestRow, right requestRow) bool {
	if left.Ordinal != right.Ordinal {
		return left.Ordinal < right.Ordinal
	}
	return strings.Compare(left.RequestID, right.RequestID) < 0
}

func requestRowPhase(record whatttft.RequestRecord) string {
	if record.Warmup {
		return requestPhaseWarmup
	}
	return requestPhaseMeasured
}

func requestRowOutcome(record whatttft.RequestRecord) string {
	if record.Error != nil {
		return requestOutcomeError
	}
	return requestOutcomeOK
}

func requestRowHTTPStatus(record whatttft.RequestRecord) string {
	if record.HTTP.StatusCode != 0 {
		return statusCodeString(record.HTTP.StatusCode)
	}
	if record.Error != nil && record.Error.StatusCode != 0 {
		return statusCodeString(record.Error.StatusCode)
	}
	return "-"
}

func requestRowErrorCategory(record whatttft.RequestRecord) string {
	if record.Error == nil {
		return "-"
	}
	if strings.TrimSpace(record.Error.Category) == "" {
		return "unknown"
	}
	return record.Error.Category
}

func requestRowFinishReason(whatttft.RequestRecord) string {
	return "-"
}

func requestRowServiceTier(record whatttft.RequestRecord, target *targetState) string {
	return firstNonEmpty(
		record.RequestedServiceTier,
		record.HTTP.RequestedServiceTier,
		targetRequestedServiceTier(target),
		record.ObservedServiceTier,
		record.HTTP.ObservedServiceTier,
		targetObservedServiceTier(target),
		"-",
	)
}

func requestRowCacheState(record whatttft.RequestRecord) string {
	if record.Cache.PromptCachedTokens != nil {
		if *record.Cache.PromptCachedTokens > 0 {
			return requestCacheHit
		}
		return requestCacheMiss
	}
	if record.Cache.CacheReadTokens != nil {
		if *record.Cache.CacheReadTokens > 0 {
			return requestCacheHit
		}
		return requestCacheMiss
	}
	if record.Cache.Hit != nil {
		if *record.Cache.Hit {
			return requestCacheHit
		}
		return requestCacheMiss
	}
	return requestCacheUnknown
}

func requestRowCachedTokens(record whatttft.RequestRecord) *int {
	if record.Cache.PromptCachedTokens != nil {
		return copyIntPointer(record.Cache.PromptCachedTokens)
	}
	if record.Cache.CacheReadTokens != nil {
		return copyIntPointer(record.Cache.CacheReadTokens)
	}
	return nil
}

func requestRowConn(record whatttft.RequestRecord) string {
	if !record.HTTP.GotConn {
		return requestConnUnknown
	}
	if record.HTTP.ConnReused {
		return requestConnReused
	}
	return requestConnNew
}

func requestRowOutputState(record whatttft.RequestRecord) string {
	if record.OutputDeltaCount == 0 {
		return requestOutputEmpty
	}
	return requestOutputDisabled
}

func targetName(target *targetState) string {
	if target == nil {
		return ""
	}
	return target.Name
}

func targetProvider(target *targetState) string {
	if target == nil {
		return ""
	}
	return target.Provider
}

func targetProviderAPI(target *targetState) string {
	if target == nil {
		return ""
	}
	return target.ProviderAPI
}

func targetModel(target *targetState) string {
	if target == nil {
		return ""
	}
	return target.Model
}

func targetRequestedServiceTier(target *targetState) string {
	if target == nil {
		return ""
	}
	return target.RequestedServiceTier
}

func targetObservedServiceTier(target *targetState) string {
	if target == nil {
		return ""
	}
	return target.ObservedServiceTier
}
