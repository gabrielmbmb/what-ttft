package tui

import "github.com/gabrielmbmb/what-ttft/pkg/whatttft"

type liveStore struct {
	benchmarkName string
	targetID      string
	targetName    string
	provider      string
	model         string
	scenarioName  string
	outputDir     string
	status        string
	reportStatus  string
	lastError     string

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
		activeRequests: make(map[string]struct{}),
		records:        make(map[string]whatttft.RequestRecord),
	}
}

func (s *liveStore) applyEvent(event whatttft.RunEvent) {
	if s.activeRequests == nil {
		s.activeRequests = make(map[string]struct{})
	}
	if s.records == nil {
		s.records = make(map[string]whatttft.RequestRecord)
	}

	s.applyEventContext(event)
	s.applyEventCounts(event)

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

	if event.Summary != nil {
		summary := copyRunSummary(*event.Summary)
		s.summary = &summary
	}
	if event.Error != nil {
		s.lastError = event.Error.Message
	}
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
	if event.Model != "" {
		s.model = event.Model
	}
	if event.ScenarioName != "" {
		s.scenarioName = event.ScenarioName
	}
	if event.OutputDir != "" {
		s.outputDir = event.OutputDir
	}
}

func (s *liveStore) applyEventCounts(event whatttft.RunEvent) {
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
	if s.completedRequests == 0 || s.completedRequests < len(s.records) {
		s.completedRequests = len(s.records)
	}
}

func (s liveStore) progress() progressSnapshot {
	return progressSnapshot{
		Total:      s.totalRequests,
		Warmup:     s.warmupRequests,
		Measured:   s.measuredRequests,
		Completed:  s.completedRequests,
		Successful: s.successfulRequests,
		Errors:     s.errorRequests,
		Active:     len(s.activeRequests),
	}
}

func (s liveStore) currentTarget() string {
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

type progressSnapshot struct {
	Total      int
	Warmup     int
	Measured   int
	Completed  int
	Successful int
	Errors     int
	Active     int
}

func copyRequestRecord(record whatttft.RequestRecord) whatttft.RequestRecord {
	copied := record
	copied.PromptTokens = copyIntPointer(record.PromptTokens)
	copied.CompletionTokens = copyIntPointer(record.CompletionTokens)
	copied.TotalTokens = copyIntPointer(record.TotalTokens)
	copied.Timeline = copyTimeline(record.Timeline)
	copied.Cache.Extra = copyAnyMap(record.Cache.Extra)
	if record.Error != nil {
		errorRecord := *record.Error
		copied.Error = &errorRecord
	}
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

func copyRunSummary(summary whatttft.RunSummary) whatttft.RunSummary {
	copied := summary
	copied.ErrorCategories = copyStringIntMap(summary.ErrorCategories)
	copied.ErrorStatusCodes = copyStringIntMap(summary.ErrorStatusCodes)
	if summary.Groups != nil {
		copied.Groups = append([]whatttft.SummaryGroup(nil), summary.Groups...)
		for index := range copied.Groups {
			copied.Groups[index].ObservedServiceTierCounts = copyStringIntMap(summary.Groups[index].ObservedServiceTierCounts)
			copied.Groups[index].ErrorCategories = copyStringIntMap(summary.Groups[index].ErrorCategories)
			copied.Groups[index].ErrorStatusCodes = copyStringIntMap(summary.Groups[index].ErrorStatusCodes)
			copied.Groups[index].Connection.ProtocolCounts = copyStringIntMap(summary.Groups[index].Connection.ProtocolCounts)
		}
	}
	return copied
}

func copyIntPointer(value *int) *int {
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
