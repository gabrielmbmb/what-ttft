package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/gabrielmbmb/what-ttft/internal/stats"
	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

const (
	metricHTTPTTFBMS                    = "http_ttfb_ms"
	metricProviderProcessingMS          = "provider_processing_ms"
	metricTTFTDeltaMS                   = "ttft_delta_ms"
	metricE2EDeltaMS                    = "e2e_delta_ms"
	metricStreamTotalMS                 = "stream_total_ms"
	metricServerWaitToFirstByteMS       = "server_wait_to_first_byte_ms"
	metricCompletionTokens              = "completion_tokens"
	metricE2EOutputTPS                  = "e2e_output_tps"
	metricGenerationDeltaOutputTPS      = "generation_delta_output_tps"
	metricStreamProtocolToFirstOutputMS = "stream_protocol_to_first_output_ms"
)

type liveStore struct {
	benchmarkName        string
	targetID             string
	targetName           string
	provider             string
	providerAPI          string
	model                string
	scenarioName         string
	cacheMode            whatttft.CacheMode
	connectionMode       whatttft.ConnectionMode
	requestedServiceTier string
	observedServiceTier  string
	outputDir            string
	saveChunks           bool
	outputCaptureStatus  outputCaptureStatus
	outputCaptureError   string
	outputCaptures       map[string]outputCapture
	status               string
	reportStatus         string
	lastError            string
	benchmarkMode        bool
	targetOrder          string
	selectedTarget       int
	targetDetail         bool
	targets              map[string]*targetState
	targetOrderIDs       []string
	hiddenTargets        map[string]bool

	totalRequests      int
	warmupRequests     int
	measuredRequests   int
	completedRequests  int
	successfulRequests int
	errorRequests      int

	activeRequests map[string]struct{}
	records        map[string]whatttft.RequestRecord
	recordOrder    []string
	summary        *whatttft.RunSummary
}

func newLiveStore() liveStore {
	return liveStore{
		activeRequests:      make(map[string]struct{}),
		records:             make(map[string]whatttft.RequestRecord),
		targets:             make(map[string]*targetState),
		hiddenTargets:       make(map[string]bool),
		outputCaptureStatus: outputCaptureStatusDisabled,
	}
}

func (s *liveStore) applyEvent(event whatttft.RunEvent) {
	if s.activeRequests == nil {
		s.activeRequests = make(map[string]struct{})
	}
	if s.records == nil {
		s.records = make(map[string]whatttft.RequestRecord)
	}
	if s.targets == nil {
		s.targets = make(map[string]*targetState)
	}
	if s.hiddenTargets == nil {
		s.hiddenTargets = make(map[string]bool)
	}

	s.applyEventContext(event)
	s.applyEventCounts(event)

	s.applyTargetEvent(event)

	switch event.Kind {
	case whatttft.EventBenchmarkStarted, whatttft.EventRunStarted:
		s.status = "running"
	case whatttft.EventTargetStarted:
		s.status = "target running"
	case whatttft.EventRequestScheduled, whatttft.EventRequestDispatched:
		if event.RequestID != "" {
			s.activeRequests[event.RequestID] = struct{}{}
		}
	case whatttft.EventRequestFinished:
		if event.RequestID != "" {
			delete(s.activeRequests, event.RequestID)
		}
		if event.Record != nil {
			s.addRecord(*event.Record)
		}
	case whatttft.EventSummaryUpdated:
		s.status = "running"
	case whatttft.EventReportWriteStarted:
		s.reportStatus = "writing reports"
	case whatttft.EventReportWriteFinished:
		s.reportStatus = "reports written"
		s.status = "completed"
	case whatttft.EventReportWriteFailed:
		s.reportStatus = "report write failed"
		s.status = "error"
	case whatttft.EventBenchmarkFinished, whatttft.EventRunFinished, whatttft.EventTargetFinished:
		s.status = "completed"
	case whatttft.EventBenchmarkCanceled, whatttft.EventRunCanceled:
		s.status = "canceled"
	case whatttft.EventBenchmarkFailed, whatttft.EventRunFailed, whatttft.EventTargetFailed:
		s.status = "error"
	}

	// Records are the authoritative source for the aggregate summary once any have arrived; a
	// per-target event summary must not clobber the cross-target aggregate. Use the event summary
	// only as a fallback before records exist.
	if event.Summary != nil && len(s.records) == 0 {
		summary := copyRunSummary(*event.Summary)
		s.summary = &summary
	}
	// When the whole run reaches a terminal state, no request can still be in flight; clear the
	// active set so a dropped request_finished event cannot leave a stuck non-zero active count.
	switch event.Kind {
	case whatttft.EventBenchmarkFinished, whatttft.EventRunFinished,
		whatttft.EventBenchmarkCanceled, whatttft.EventRunCanceled,
		whatttft.EventBenchmarkFailed, whatttft.EventRunFailed:
		s.activeRequests = make(map[string]struct{})
	}
	if event.Kind == whatttft.EventBenchmarkCanceled {
		s.markUnfinishedTargets(targetStatusCanceled)
	}
	if event.Kind == whatttft.EventBenchmarkFailed {
		s.markUnfinishedTargets(targetStatusFailed)
	}
	if event.Error != nil {
		s.lastError = event.Error.Message
	}
}

func (s *liveStore) markUnfinishedTargets(status string) {
	for _, targetID := range s.targetOrderIDs {
		state := s.targets[targetID]
		if state == nil || state.Status == targetStatusFinished {
			continue
		}
		state.Status = status
		state.Active = 0
	}
}

func (s *liveStore) applyTargetEvent(event whatttft.RunEvent) {
	if event.Kind == whatttft.EventBenchmarkStarted || len(event.Targets) > 0 || (event.BenchmarkName != "" && event.TargetID != "") {
		s.benchmarkMode = true
	}
	if event.Kind == whatttft.EventBenchmarkStarted {
		s.targetOrder = firstNonEmpty(string(event.TargetOrder), string(whatttft.SerialTargetOrder))
	}
	for _, eventTarget := range event.Targets {
		state := s.ensureTarget(eventTarget.TargetID)
		state.Name = eventTarget.TargetName
		state.Provider = eventTarget.Provider
		state.ProviderAPI = eventTarget.ProviderAPI
		state.Model = eventTarget.Model
		state.RequestedServiceTier = eventTarget.RequestedServiceTier
		state.ScenarioName = eventTarget.ScenarioName
		state.CacheMode = eventTarget.CacheMode
		state.ConnectionMode = eventTarget.ConnectionMode
		state.Total = eventTarget.TotalRequests
		state.Warmup = eventTarget.WarmupRequests
		state.Measured = eventTarget.MeasuredRequests
		state.Concurrency = eventTarget.Concurrency
		if state.Status == "" {
			state.Status = targetStatusPending
		}
	}

	if event.TargetID == "" {
		return
	}
	state := s.ensureTarget(event.TargetID)
	state.applyEventContext(event)
	if (event.Kind == whatttft.EventRequestScheduled || event.Kind == whatttft.EventRequestDispatched) && event.RequestID != "" {
		if _, exists := s.activeRequests[event.RequestID]; !exists {
			state.Active++
		}
	}
	if event.Kind == whatttft.EventRequestFinished && event.RequestID != "" && state.Active > 0 {
		state.Active--
	}
	state.applyEventPlanCounts(event)
	// target_finished carries this target's authoritative outcome counts, so it may set them even
	// though target_started (which has none yet) must not.
	if event.Kind != whatttft.EventTargetStarted {
		state.applyEventOutcomeCounts(event)
	}

	switch event.Kind {
	case whatttft.EventTargetStarted, whatttft.EventRunStarted, whatttft.EventRequestScheduled, whatttft.EventRequestDispatched:
		state.Status = targetStatusRunning
	case whatttft.EventTargetFinished, whatttft.EventRunFinished:
		state.Status = targetStatusFinished
		state.Active = 0
	case whatttft.EventTargetFailed, whatttft.EventRunFailed:
		state.Status = targetStatusFailed
	case whatttft.EventRunCanceled:
		state.Status = targetStatusCanceled
		state.Active = 0
	}
	if event.Error != nil {
		state.LastError = event.Error.Message
	}
}

func (s *liveStore) ensureTarget(targetID string) *targetState {
	if strings.TrimSpace(targetID) == "" {
		targetID = "target"
	}
	if s.targets == nil {
		s.targets = make(map[string]*targetState)
	}
	if state, ok := s.targets[targetID]; ok {
		return state
	}
	state := &targetState{ID: targetID, Status: targetStatusPending}
	s.targets[targetID] = state
	s.targetOrderIDs = append(s.targetOrderIDs, targetID)
	return state
}

func (s *liveStore) applyEventContext(event whatttft.RunEvent) {
	if event.BenchmarkName != "" {
		s.benchmarkName = event.BenchmarkName
	}
	if event.TargetID != "" {
		s.targetID = event.TargetID
	}
	if event.TargetName != "" {
		s.targetName = event.TargetName
	}
	if event.Provider != "" {
		s.provider = event.Provider
	}
	if event.ProviderAPI != "" {
		s.providerAPI = event.ProviderAPI
	}
	if event.Model != "" {
		s.model = event.Model
	}
	if event.ScenarioName != "" {
		s.scenarioName = event.ScenarioName
	}
	if event.CacheMode != "" {
		s.cacheMode = event.CacheMode
	}
	if event.ConnectionMode != "" {
		s.connectionMode = event.ConnectionMode
	}
	if event.RequestedServiceTier != "" {
		s.requestedServiceTier = event.RequestedServiceTier
	}
	if event.OutputDir != "" {
		s.outputDir = event.OutputDir
	}
	s.applyOutputCaptureEvent(event)
}

func (s *liveStore) applyOutputCaptureEvent(event whatttft.RunEvent) {
	switch event.Kind {
	case whatttft.EventRunStarted, whatttft.EventBenchmarkStarted:
		s.configureOutputCapture(event.SaveChunks)
	case whatttft.EventTargetStarted:
		if event.SaveChunks {
			s.configureOutputCapture(true)
		}
	case whatttft.EventReportWriteStarted, whatttft.EventReportWriteFinished:
		if event.SaveChunks {
			s.configureOutputCapture(true)
		} else if s.outputCaptureStatus == "" {
			s.configureOutputCapture(false)
		}
	}
}

func (s *liveStore) applyEventCounts(event whatttft.RunEvent) {
	if s.benchmarkMode && event.TargetID != "" {
		return
	}
	if event.TotalRequests != 0 {
		s.totalRequests = event.TotalRequests
	}
	if event.WarmupRequests != 0 {
		s.warmupRequests = event.WarmupRequests
	}
	if event.MeasuredRequests != 0 {
		s.measuredRequests = event.MeasuredRequests
	}
	if event.CompletedRequests != 0 {
		s.completedRequests = event.CompletedRequests
	}
	if event.SuccessfulRequests != 0 {
		s.successfulRequests = event.SuccessfulRequests
	}
	if event.ErrorRequests != 0 {
		s.errorRequests = event.ErrorRequests
	}
}

func (s *liveStore) addRecord(record whatttft.RequestRecord) {
	copied := copyRequestRecord(record)
	if _, exists := s.records[copied.RequestID]; !exists {
		s.recordOrder = append(s.recordOrder, copied.RequestID)
	}
	s.records[copied.RequestID] = copied
	s.applyRecordContext(copied)
	s.applyRecordTargetContext(copied)
	if s.completedRequests == 0 || s.completedRequests < len(s.records) {
		s.completedRequests = len(s.records)
	}
	s.recomputeSummary()
}

// recomputeSummary rebuilds the cached summary from the current records once, so per-frame
// rendering can read it in O(1) instead of re-running Summarize over every record each frame.
// Without this cache the render loop falls behind on large multi-target runs, which causes the
// bounded event bus to drop request and terminal events and the dashboard to appear stuck.
func (s *liveStore) recomputeSummary() {
	records := make([]whatttft.RequestRecord, 0, len(s.recordOrder))
	for _, requestID := range s.recordOrder {
		if record, ok := s.records[requestID]; ok {
			records = append(records, record)
		}
	}
	summary := whatttft.Summarize(records)
	s.summary = &summary
}

func (s *liveStore) applyRecordTargetContext(record whatttft.RequestRecord) {
	if record.TargetID == "" {
		return
	}
	state := s.ensureTarget(record.TargetID)
	if state.Name == "" {
		state.Name = record.TargetName
	}
	if state.Provider == "" {
		state.Provider = record.Provider
	}
	if state.Model == "" {
		state.Model = record.Model
	}
	if state.ScenarioName == "" {
		state.ScenarioName = record.ScenarioName
	}
	if state.CacheMode == "" {
		state.CacheMode = record.CacheMode
	}
	if state.ConnectionMode == "" {
		state.ConnectionMode = record.ConnectionMode
	}
	if state.RequestedServiceTier == "" {
		state.RequestedServiceTier = record.RequestedServiceTier
	}
	if state.RequestedServiceTier == "" {
		state.RequestedServiceTier = record.HTTP.RequestedServiceTier
	}
	if state.ObservedServiceTier == "" {
		state.ObservedServiceTier = record.ObservedServiceTier
	}
	if state.ObservedServiceTier == "" {
		state.ObservedServiceTier = record.HTTP.ObservedServiceTier
	}
	if state.Total == 0 {
		state.Total = state.Warmup + state.Measured
	}
}

func (s *liveStore) applyRecordContext(record whatttft.RequestRecord) {
	if s.provider == "" && record.Provider != "" {
		s.provider = record.Provider
	}
	if s.model == "" && record.Model != "" {
		s.model = record.Model
	}
	if s.scenarioName == "" && record.ScenarioName != "" {
		s.scenarioName = record.ScenarioName
	}
	if s.cacheMode == "" && record.CacheMode != "" {
		s.cacheMode = record.CacheMode
	}
	if s.connectionMode == "" && record.ConnectionMode != "" {
		s.connectionMode = record.ConnectionMode
	}
	if s.requestedServiceTier == "" && record.RequestedServiceTier != "" {
		s.requestedServiceTier = record.RequestedServiceTier
	}
	if s.requestedServiceTier == "" && record.HTTP.RequestedServiceTier != "" {
		s.requestedServiceTier = record.HTTP.RequestedServiceTier
	}
	if s.observedServiceTier == "" && record.ObservedServiceTier != "" {
		s.observedServiceTier = record.ObservedServiceTier
	}
	if s.observedServiceTier == "" && record.HTTP.ObservedServiceTier != "" {
		s.observedServiceTier = record.HTTP.ObservedServiceTier
	}
}

// Progress returns a copy of the current request progress counters tracked from events and completed records.
func (s liveStore) Progress() progressSnapshot {
	if s.IsBenchmark() && len(s.targetOrderIDs) > 0 {
		return s.benchmarkProgress()
	}

	completed := s.completedRequests
	successful := s.successfulRequests
	errors := s.errorRequests
	records := s.completedRecords()
	if len(records) > completed {
		completed = len(records)
	}
	computedSuccessful, computedErrors := measuredOutcomeCounts(records)
	if computedSuccessful > successful {
		successful = computedSuccessful
	}
	if computedErrors > errors {
		errors = computedErrors
	}

	return progressSnapshot{
		Total:      s.totalRequests,
		Warmup:     s.warmupRequests,
		Measured:   s.measuredRequests,
		Completed:  completed,
		Successful: successful,
		Errors:     errors,
		Active:     len(s.activeRequests),
	}
}

func (s liveStore) benchmarkProgress() progressSnapshot {
	progress := progressSnapshot{Total: s.totalRequests, Warmup: s.warmupRequests, Measured: s.measuredRequests, Active: len(s.activeRequests)}
	for _, row := range s.TargetRows() {
		progress.Completed += row.Completed
		progress.Successful += row.Successful
		progress.Errors += row.Errors
	}
	if progress.Completed == 0 {
		progress.Completed = len(s.completedRecords())
	}
	if progress.Successful == 0 && progress.Errors == 0 {
		progress.Successful, progress.Errors = measuredOutcomeCounts(s.completedRecords())
	}
	return progress
}

func (s liveStore) progress() progressSnapshot {
	return s.Progress()
}

// CurrentTarget returns the best current target label from target ID and target name fields.
func (s liveStore) CurrentTarget() string {
	if s.targetID != "" && s.targetName != "" {
		return s.targetID + " (" + s.targetName + ")"
	}
	if s.targetID != "" {
		return s.targetID
	}
	if s.targetName != "" {
		return s.targetName
	}
	return "-"
}

func (s liveStore) currentTarget() string {
	return s.CurrentTarget()
}

// IsBenchmark reports whether the live event stream represents a multi-target benchmark.
func (s liveStore) IsBenchmark() bool {
	return s.benchmarkMode || s.benchmarkName != "" && len(s.targetOrderIDs) > 0
}

// TargetRows returns target progress rows in configured benchmark order.
func (s liveStore) TargetRows() []targetRow {
	rows := make([]targetRow, 0, len(s.targetOrderIDs))
	groups := groupsByTargetID(s.Groups())
	for _, targetID := range s.targetOrderIDs {
		state := s.targets[targetID]
		if state == nil {
			continue
		}
		row := state.row()
		row.Visible = s.targetVisible(targetID)
		if group := groups[targetID]; group != nil {
			row.Successful = max(row.Successful, group.SuccessfulRequests)
			row.Errors = max(row.Errors, group.ErrorRequests)
		}
		records := s.recordsForTarget(targetID)
		if len(records) > row.Completed {
			row.Completed = len(records)
		}
		computedSuccessful, computedErrors := measuredOutcomeCounts(records)
		if computedSuccessful > row.Successful {
			row.Successful = computedSuccessful
		}
		if computedErrors > row.Errors {
			row.Errors = computedErrors
		}
		rows = append(rows, row)
	}
	return rows
}

func (s liveStore) selectedTargetID() string {
	if len(s.targetOrderIDs) == 0 {
		return ""
	}
	index := s.selectedTarget
	if index < 0 {
		index = 0
	}
	if index >= len(s.targetOrderIDs) {
		index = len(s.targetOrderIDs) - 1
	}
	return s.targetOrderIDs[index]
}

func (s *liveStore) selectTarget(delta int) {
	if s == nil || len(s.targetOrderIDs) == 0 {
		return
	}
	s.selectedTarget += delta
	if s.selectedTarget < 0 {
		s.selectedTarget = 0
	}
	if s.selectedTarget >= len(s.targetOrderIDs) {
		s.selectedTarget = len(s.targetOrderIDs) - 1
	}
}

func (s *liveStore) setTargetDetail(detail bool) {
	if s == nil {
		return
	}
	s.targetDetail = detail
}

func (s *liveStore) toggleSelectedTargetVisibility() {
	if s == nil {
		return
	}
	targetID := s.selectedTargetID()
	if targetID == "" {
		return
	}
	if s.hiddenTargets == nil {
		s.hiddenTargets = make(map[string]bool)
	}
	if s.hiddenTargets[targetID] {
		delete(s.hiddenTargets, targetID)
		return
	}
	if s.visibleTargetCount() <= 1 {
		return
	}
	s.hiddenTargets[targetID] = true
}

func (s *liveStore) showAllTargets() {
	if s == nil {
		return
	}
	s.hiddenTargets = make(map[string]bool)
}

func (s liveStore) targetVisible(targetID string) bool {
	if strings.TrimSpace(targetID) == "" {
		return true
	}
	return !s.hiddenTargets[targetID]
}

func (s liveStore) visibleTargetCount() int {
	count := 0
	for _, targetID := range s.targetOrderIDs {
		if s.targetVisible(targetID) {
			count++
		}
	}
	return count
}

func (s liveStore) selectedTargetStore() liveStore {
	targetID := s.selectedTargetID()
	if targetID == "" {
		return s
	}
	selected := s
	selected.records = make(map[string]whatttft.RequestRecord)
	selected.recordOrder = nil
	selected.activeRequests = make(map[string]struct{})
	for _, requestID := range s.recordOrder {
		record, ok := s.records[requestID]
		if !ok || record.TargetID != targetID {
			continue
		}
		selected.records[requestID] = copyRequestRecord(record)
		selected.recordOrder = append(selected.recordOrder, requestID)
	}
	// Recompute the cached summary over just this target's records, since summarySnapshot now
	// reads the cache rather than recomputing from records on demand.
	(&selected).recomputeSummary()
	selected.targetID = targetID
	if state := s.targets[targetID]; state != nil {
		selected.targetName = state.Name
		selected.provider = state.Provider
		selected.model = state.Model
		selected.scenarioName = state.ScenarioName
		selected.cacheMode = state.CacheMode
		selected.connectionMode = state.ConnectionMode
		selected.requestedServiceTier = state.RequestedServiceTier
		selected.observedServiceTier = state.ObservedServiceTier
	}
	return selected
}

// recordsForTarget returns one target's records in arrival order. Like completedRecords, the
// records are shared and must be treated as read-only (single-goroutine, render-hot path).
func (s liveStore) recordsForTarget(targetID string) []whatttft.RequestRecord {
	records := make([]whatttft.RequestRecord, 0)
	for _, requestID := range s.recordOrder {
		record, ok := s.records[requestID]
		if !ok || record.TargetID != targetID {
			continue
		}
		records = append(records, record)
	}
	return records
}

// Groups returns summary groups sorted in the same stable order as whatttft.Summarize.
func (s liveStore) Groups() []whatttft.SummaryGroup {
	summary := s.summarySnapshot()
	return copyRunSummary(summary).Groups
}

// MetricRows returns dashboard metric rows for core latency and throughput metrics over successful measured requests.
func (s liveStore) MetricRows() []metricRow {
	return []metricRow{
		metricRowFromValues(metricHTTPTTFBMS, "ms", s.metricValues(metricHTTPTTFBMS)),
		metricRowFromValues(metricProviderProcessingMS, "ms", s.metricValues(metricProviderProcessingMS)),
		metricRowFromValues(metricServerWaitToFirstByteMS, "ms", s.metricValues(metricServerWaitToFirstByteMS)),
		metricRowFromValues(metricTTFTDeltaMS, "ms", s.metricValues(metricTTFTDeltaMS)),
		metricRowFromValues(metricE2EDeltaMS, "ms", s.metricValues(metricE2EDeltaMS)),
		metricRowFromValues(metricCompletionTokens, "tokens", s.completionTokenValues()),
		metricRowFromValues(metricE2EOutputTPS, "tokens/s", s.metricValues(metricE2EOutputTPS)),
		metricRowFromValues(metricGenerationDeltaOutputTPS, "tokens/s", s.metricValues(metricGenerationDeltaOutputTPS)),
	}
}

// SlowestRequests returns the n slowest successful measured request/metric pairs by observed milliseconds.
func (s liveStore) SlowestRequests(n int) []slowRequest {
	if n <= 0 {
		return nil
	}

	var requests []slowRequest
	for _, record := range s.completedRecords() {
		if record.Warmup || record.Error != nil {
			continue
		}
		appendSlowRequestMetric(&requests, record, metricTTFTDeltaMS, record.Derived.TTFTDeltaMS)
		appendSlowRequestMetric(&requests, record, metricE2EDeltaMS, record.Derived.E2EDeltaMS)
		appendSlowRequestMetric(&requests, record, metricStreamTotalMS, record.Derived.StreamTotalMS)
	}
	sort.Slice(requests, func(i int, j int) bool {
		if requests[i].ValueMS != requests[j].ValueMS {
			return requests[i].ValueMS > requests[j].ValueMS
		}
		if requests[i].RequestID != requests[j].RequestID {
			return requests[i].RequestID < requests[j].RequestID
		}
		return requests[i].MetricName < requests[j].MetricName
	})
	if len(requests) > n {
		requests = requests[:n]
	}

	return requests
}

// StatusCounts returns copied HTTP status-code and error-category counts for completed measured requests.
func (s liveStore) StatusCounts() statusCounts {
	counts := statusCounts{ErrorCategories: make(map[string]int), StatusCodes: make(map[string]int)}
	for _, record := range s.completedRecords() {
		if record.Warmup {
			continue
		}
		if record.HTTP.StatusCode != 0 {
			counts.StatusCodes[statusCodeString(record.HTTP.StatusCode)]++
		}
		if record.Error != nil {
			category := record.Error.Category
			if category == "" {
				category = "unknown"
			}
			counts.ErrorCategories[category]++
		}
	}
	if len(counts.ErrorCategories) == 0 {
		counts.ErrorCategories = nil
	}
	if len(counts.StatusCodes) == 0 {
		counts.StatusCodes = nil
	}

	return counts
}

func (s liveStore) summarySnapshot() whatttft.RunSummary {
	// The summary is cached and refreshed on each record arrival (see recomputeSummary), so this
	// stays O(number of groups) per call even when it is invoked several times per render frame.
	if s.summary != nil {
		return copyRunSummary(*s.summary)
	}
	return whatttft.RunSummary{}
}

// completedRecords returns the recorded requests in arrival order. The records are shared, not
// deep-copied: the store is only accessed from the single Bubble Tea goroutine and every caller is
// read-only, so copying every record on each call (this is invoked many times per render frame)
// was pure overhead that made large multi-target runs fall behind and drop live events.
func (s liveStore) completedRecords() []whatttft.RequestRecord {
	records := make([]whatttft.RequestRecord, 0, len(s.records))
	for _, requestID := range s.recordOrder {
		record, ok := s.records[requestID]
		if !ok {
			continue
		}
		records = append(records, record)
	}

	return records
}

func measuredOutcomeCounts(records []whatttft.RequestRecord) (int, int) {
	successful := 0
	errors := 0
	for _, record := range records {
		if record.Warmup {
			continue
		}
		if record.Error == nil {
			successful++
		} else {
			errors++
		}
	}
	return successful, errors
}

// RunSeries returns successful measured request metric values in completion order.
func (s liveStore) RunSeries(name string) []float64 {
	return s.metricValues(name)
}

func (s liveStore) completionTokenValues() []float64 {
	var values []float64
	for _, record := range s.completedRecords() {
		if record.Warmup || record.Error != nil || record.CompletionTokens == nil {
			continue
		}
		values = append(values, float64(*record.CompletionTokens))
	}

	return values
}

func (s liveStore) metricValues(name string) []float64 {
	var values []float64
	for _, record := range s.completedRecords() {
		if record.Warmup || record.Error != nil {
			continue
		}
		appendMetricValue(&values, metricValue(record, name))
	}

	return values
}

const (
	targetStatusPending  = "pending"
	targetStatusRunning  = "running"
	targetStatusFinished = "finished"
	targetStatusFailed   = "failed"
	targetStatusCanceled = "canceled"
)

type progressSnapshot struct {
	Total      int
	Warmup     int
	Measured   int
	Completed  int
	Successful int
	Errors     int
	Active     int
}

type targetState struct {
	ID                   string
	Name                 string
	Provider             string
	ProviderAPI          string
	Model                string
	ScenarioName         string
	CacheMode            whatttft.CacheMode
	ConnectionMode       whatttft.ConnectionMode
	RequestedServiceTier string
	ObservedServiceTier  string
	Status               string
	LastError            string
	Total                int
	Warmup               int
	Measured             int
	Completed            int
	Successful           int
	Errors               int
	Active               int
	Concurrency          int
}

type targetRow struct {
	ID                   string
	Name                 string
	Provider             string
	ProviderAPI          string
	Model                string
	ScenarioName         string
	CacheMode            whatttft.CacheMode
	ConnectionMode       whatttft.ConnectionMode
	RequestedServiceTier string
	ObservedServiceTier  string
	Status               string
	Total                int
	Warmup               int
	Measured             int
	Completed            int
	Successful           int
	Errors               int
	Active               int
	Concurrency          int
	Visible              bool
}

func (s *targetState) applyEventContext(event whatttft.RunEvent) {
	if event.TargetName != "" {
		s.Name = event.TargetName
	}
	if event.Provider != "" {
		s.Provider = event.Provider
	}
	if event.ProviderAPI != "" {
		s.ProviderAPI = event.ProviderAPI
	}
	if event.Model != "" {
		s.Model = event.Model
	}
	if event.ScenarioName != "" {
		s.ScenarioName = event.ScenarioName
	}
	if event.CacheMode != "" {
		s.CacheMode = event.CacheMode
	}
	if event.ConnectionMode != "" {
		s.ConnectionMode = event.ConnectionMode
	}
	if event.RequestedServiceTier != "" {
		s.RequestedServiceTier = event.RequestedServiceTier
	}
	if event.Concurrency != 0 {
		s.Concurrency = event.Concurrency
	}
}

func (s *targetState) applyEventPlanCounts(event whatttft.RunEvent) {
	if event.TotalRequests != 0 {
		s.Total = event.TotalRequests
	}
	if event.WarmupRequests != 0 {
		s.Warmup = event.WarmupRequests
	}
	if event.MeasuredRequests != 0 {
		s.Measured = event.MeasuredRequests
	}
}

func (s *targetState) applyEventOutcomeCounts(event whatttft.RunEvent) {
	if event.CompletedRequests != 0 {
		s.Completed = event.CompletedRequests
	}
	if event.SuccessfulRequests != 0 {
		s.Successful = event.SuccessfulRequests
	}
	if event.ErrorRequests != 0 {
		s.Errors = event.ErrorRequests
	}
}

func targetStatusOrPending(status string) string {
	if strings.TrimSpace(status) == "" {
		return targetStatusPending
	}
	return status
}

func (s targetState) row() targetRow {
	return targetRow{
		ID:                   s.ID,
		Name:                 s.Name,
		Provider:             s.Provider,
		ProviderAPI:          s.ProviderAPI,
		Model:                s.Model,
		ScenarioName:         s.ScenarioName,
		CacheMode:            s.CacheMode,
		ConnectionMode:       s.ConnectionMode,
		RequestedServiceTier: s.RequestedServiceTier,
		ObservedServiceTier:  s.ObservedServiceTier,
		Status:               targetStatusOrPending(s.Status),
		Total:                s.Total,
		Warmup:               s.Warmup,
		Measured:             s.Measured,
		Completed:            s.Completed,
		Successful:           s.Successful,
		Errors:               s.Errors,
		Active:               s.Active,
		Concurrency:          s.Concurrency,
	}
}

type metricRow struct {
	Name  string
	Unit  string
	Count int
	P50   *float64
	P95   *float64
	P99   *float64
	Mean  *float64
}

type slowRequest struct {
	RequestID  string
	TargetID   string
	MetricName string
	ValueMS    float64
	Record     whatttft.RequestRecord
}

type statusCounts struct {
	ErrorCategories map[string]int
	StatusCodes     map[string]int
}

func groupsByTargetID(groups []whatttft.SummaryGroup) map[string]*whatttft.SummaryGroup {
	byTargetID := make(map[string]*whatttft.SummaryGroup, len(groups))
	for index := range groups {
		group := groups[index]
		if group.TargetID == "" {
			continue
		}
		copied := group
		byTargetID[group.TargetID] = &copied
	}
	return byTargetID
}

func metricRowFromValues(name string, unit string, values []float64) metricRow {
	distribution := stats.Summarize(values)
	return metricRow{
		Name:  name,
		Unit:  unit,
		Count: distribution.Count,
		P50:   copyFloat64Pointer(distribution.P50),
		P95:   copyFloat64Pointer(distribution.P95),
		P99:   copyFloat64Pointer(distribution.P99),
		Mean:  copyFloat64Pointer(distribution.Mean),
	}
}

func appendSlowRequestMetric(requests *[]slowRequest, record whatttft.RequestRecord, name string, value *float64) {
	if value == nil {
		return
	}
	*requests = append(*requests, slowRequest{
		RequestID:  record.RequestID,
		TargetID:   record.TargetID,
		MetricName: name,
		ValueMS:    *value,
		Record:     copyRequestRecord(record),
	})
}

func metricValue(record whatttft.RequestRecord, name string) *float64 {
	switch name {
	case metricHTTPTTFBMS:
		return record.Derived.HTTPTTFBMS
	case metricProviderProcessingMS:
		return record.HTTP.ProviderProcessingMS
	case metricTTFTDeltaMS:
		return record.Derived.TTFTDeltaMS
	case metricE2EDeltaMS:
		return record.Derived.E2EDeltaMS
	case metricStreamTotalMS:
		return record.Derived.StreamTotalMS
	case metricServerWaitToFirstByteMS:
		return record.Derived.ServerWaitToFirstByteMS
	case metricE2EOutputTPS:
		return record.Derived.E2EOutputTPS
	case metricGenerationDeltaOutputTPS:
		return record.Derived.GenerationDeltaOutputTPS
	case metricStreamProtocolToFirstOutputMS:
		return record.Derived.StreamProtocolToFirstOutputMS
	default:
		return nil
	}
}

func appendMetricValue(values *[]float64, value *float64) {
	if value != nil {
		*values = append(*values, *value)
	}
}

func statusCodeString(statusCode int) string {
	return strconv.Itoa(statusCode)
}

func copyRequestRecord(record whatttft.RequestRecord) whatttft.RequestRecord {
	copied := record
	copied.PromptTokens = copyIntPointer(record.PromptTokens)
	copied.CompletionTokens = copyIntPointer(record.CompletionTokens)
	copied.TotalTokens = copyIntPointer(record.TotalTokens)
	copied.Cache = copyCacheRecord(record.Cache)
	copied.HTTP = copyHTTPRecord(record.HTTP)
	copied.Timeline = copyTimeline(record.Timeline)
	copied.Derived = copyDerivedMetrics(record.Derived)
	if record.Error != nil {
		errorRecord := *record.Error
		copied.Error = &errorRecord
	}
	return copied
}

func copyCacheRecord(record whatttft.CacheRecord) whatttft.CacheRecord {
	copied := record
	copied.Hit = copyBoolPointer(record.Hit)
	copied.PromptCachedTokens = copyIntPointer(record.PromptCachedTokens)
	copied.CacheReadTokens = copyIntPointer(record.CacheReadTokens)
	copied.CacheCreationTokens = copyIntPointer(record.CacheCreationTokens)
	copied.CacheTTLSeconds = copyInt64Pointer(record.CacheTTLSeconds)
	copied.Extra = copyAnyMap(record.Extra)
	return copied
}

func copyHTTPRecord(record whatttft.HTTPRecord) whatttft.HTTPRecord {
	copied := record
	copied.ProviderProcessingMS = copyFloat64Pointer(record.ProviderProcessingMS)
	return copied
}

func copyTimeline(timeline whatttft.Timeline) whatttft.Timeline {
	copied := timeline
	if timeline.EventsNS != nil {
		copied.EventsNS = make(map[whatttft.EventName]int64, len(timeline.EventsNS))
		for key, value := range timeline.EventsNS {
			copied.EventsNS[key] = value
		}
	}
	return copied
}

func copyDerivedMetrics(metrics whatttft.DerivedMetrics) whatttft.DerivedMetrics {
	return whatttft.DerivedMetrics{
		HTTPTTFBMS:                    copyFloat64Pointer(metrics.HTTPTTFBMS),
		HeadersLatencyMS:              copyFloat64Pointer(metrics.HeadersLatencyMS),
		FirstEventMS:                  copyFloat64Pointer(metrics.FirstEventMS),
		TTFTDeltaMS:                   copyFloat64Pointer(metrics.TTFTDeltaMS),
		E2EDeltaMS:                    copyFloat64Pointer(metrics.E2EDeltaMS),
		StreamTotalMS:                 copyFloat64Pointer(metrics.StreamTotalMS),
		GenerationDeltaMS:             copyFloat64Pointer(metrics.GenerationDeltaMS),
		E2EOutputTPS:                  copyFloat64Pointer(metrics.E2EOutputTPS),
		GenerationDeltaOutputTPS:      copyFloat64Pointer(metrics.GenerationDeltaOutputTPS),
		ServerWaitToFirstByteMS:       copyFloat64Pointer(metrics.ServerWaitToFirstByteMS),
		StreamProtocolToFirstOutputMS: copyFloat64Pointer(metrics.StreamProtocolToFirstOutputMS),
		DNSMS:                         copyFloat64Pointer(metrics.DNSMS),
		TCPConnectMS:                  copyFloat64Pointer(metrics.TCPConnectMS),
		TLSMS:                         copyFloat64Pointer(metrics.TLSMS),
		RequestWriteMS:                copyFloat64Pointer(metrics.RequestWriteMS),
	}
}

func copyRunSummary(summary whatttft.RunSummary) whatttft.RunSummary {
	copied := summary
	copied.ErrorCategories = copyStringIntMap(summary.ErrorCategories)
	copied.ErrorStatusCodes = copyStringIntMap(summary.ErrorStatusCodes)
	if summary.Groups != nil {
		copied.Groups = append([]whatttft.SummaryGroup(nil), summary.Groups...)
		for index := range copied.Groups {
			copied.Groups[index] = copySummaryGroup(summary.Groups[index])
		}
	}
	return copied
}

func copySummaryGroup(group whatttft.SummaryGroup) whatttft.SummaryGroup {
	copied := group
	copied.ObservedServiceTierCounts = copyStringIntMap(group.ObservedServiceTierCounts)
	copied.ErrorCategories = copyStringIntMap(group.ErrorCategories)
	copied.ErrorStatusCodes = copyStringIntMap(group.ErrorStatusCodes)
	copied.Metrics = copyMetricDistributions(group.Metrics)
	copied.SystemTPS = copyFloat64Pointer(group.SystemTPS)
	copied.RPS = copyFloat64Pointer(group.RPS)
	copied.Cache = copyCacheSummary(group.Cache)
	copied.Connection = copyConnectionSummary(group.Connection)
	return copied
}

func copyMetricDistributions(metrics whatttft.MetricDistributions) whatttft.MetricDistributions {
	return whatttft.MetricDistributions{
		HTTPTTFBMS:                          copyDistribution(metrics.HTTPTTFBMS),
		HeadersLatencyMS:                    copyDistribution(metrics.HeadersLatencyMS),
		FirstEventMS:                        copyDistribution(metrics.FirstEventMS),
		TTFTDeltaMS:                         copyDistribution(metrics.TTFTDeltaMS),
		E2EDeltaMS:                          copyDistribution(metrics.E2EDeltaMS),
		StreamTotalMS:                       copyDistribution(metrics.StreamTotalMS),
		GenerationDeltaMS:                   copyDistribution(metrics.GenerationDeltaMS),
		ProviderProcessingMS:                copyDistribution(metrics.ProviderProcessingMS),
		ServerWaitToFirstByteMS:             copyDistribution(metrics.ServerWaitToFirstByteMS),
		ServerWaitMinusProviderProcessingMS: copyDistribution(metrics.ServerWaitMinusProviderProcessingMS),
		StreamProtocolToFirstOutputMS:       copyDistribution(metrics.StreamProtocolToFirstOutputMS),
		DNSMS:                               copyDistribution(metrics.DNSMS),
		TCPConnectMS:                        copyDistribution(metrics.TCPConnectMS),
		TLSMS:                               copyDistribution(metrics.TLSMS),
		RequestWriteMS:                      copyDistribution(metrics.RequestWriteMS),
		CompletionTokens:                    copyDistribution(metrics.CompletionTokens),
		E2EOutputTPS:                        copyDistribution(metrics.E2EOutputTPS),
		GenerationDeltaOutputTPS:            copyDistribution(metrics.GenerationDeltaOutputTPS),
	}
}

func copyCacheSummary(summary whatttft.CacheSummary) whatttft.CacheSummary {
	copied := summary
	copied.CachedPromptTokens = copyDistribution(summary.CachedPromptTokens)
	return copied
}

func copyConnectionSummary(summary whatttft.ConnectionSummary) whatttft.ConnectionSummary {
	copied := summary
	copied.ProtocolCounts = copyStringIntMap(summary.ProtocolCounts)
	return copied
}

func copyDistribution(distribution whatttft.Distribution) whatttft.Distribution {
	return whatttft.Distribution{
		Count:  distribution.Count,
		Min:    copyFloat64Pointer(distribution.Min),
		Mean:   copyFloat64Pointer(distribution.Mean),
		P50:    copyFloat64Pointer(distribution.P50),
		P90:    copyFloat64Pointer(distribution.P90),
		P95:    copyFloat64Pointer(distribution.P95),
		P99:    copyFloat64Pointer(distribution.P99),
		Max:    copyFloat64Pointer(distribution.Max),
		StdDev: copyFloat64Pointer(distribution.StdDev),
	}
}

func copyIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyStringIntMap(values map[string]int) map[string]int {
	if values == nil {
		return nil
	}
	copied := make(map[string]int, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
