package tui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type metricRangeFilter struct {
	Name  string
	Op    string
	Value float64
}

type requestFilters struct {
	Query                  string
	TargetIDs              map[string]bool
	Models                 map[string]bool
	ProviderAPIs           map[string]bool
	Phases                 map[string]bool
	Outcomes               map[string]bool
	HTTPStatuses           map[string]bool
	HTTPClasses            map[string]bool
	ErrorCategories        map[string]bool
	CacheStates            map[string]bool
	MetricRanges           []metricRangeFilter
	RequestIDSubstr        string
	RespectChartVisibility bool
	ChartVisibilitySet     bool
}

func parseRequestFilterQuery(query string) (requestFilters, requestSort, error) {
	filters := requestFilters{RespectChartVisibility: true}
	sortOrder := requestSortCompletionOrder
	query = strings.TrimSpace(query)
	if query == "" {
		return filters, sortOrder, nil
	}

	var bare []string
	for _, token := range strings.Fields(query) {
		if strings.EqualFold(token, "clear") {
			filters = requestFilters{RespectChartVisibility: true}
			sortOrder = requestSortCompletionOrder
			bare = nil
			continue
		}
		if metric, ok, err := parseMetricRangeToken(token); ok || err != nil {
			if err != nil {
				return filters, sortOrder, err
			}
			filters.MetricRanges = append(filters.MetricRanges, metric)
			continue
		}
		key, value, ok := strings.Cut(token, ":")
		if !ok {
			bare = append(bare, token)
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if value == "" {
			return filters, sortOrder, fmt.Errorf("filter %q requires a value", key)
		}
		var err error
		sortOrder, err = applyRequestFilterToken(&filters, sortOrder, key, value)
		if err != nil {
			return filters, sortOrder, err
		}
	}
	if len(bare) > 0 {
		filters.Query = strings.Join(bare, " ")
	}
	return filters, sortOrder, nil
}

func applyRequestFilterToken(filters *requestFilters, sortOrder requestSort, key string, value string) (requestSort, error) {
	switch key {
	case "model":
		addFilterValue(&filters.Models, value)
	case "target", "target_id", "target-id":
		addFilterValue(&filters.TargetIDs, value)
	case "api", "provider_api", "provider-api":
		addFilterValue(&filters.ProviderAPIs, value)
	case "phase":
		phase, err := normalizeRequestPhase(value)
		if err != nil {
			return sortOrder, err
		}
		addFilterValue(&filters.Phases, phase)
	case "warmup":
		phase, err := phaseFromWarmupFilter(value)
		if err != nil {
			return sortOrder, err
		}
		addFilterValue(&filters.Phases, phase)
	case "outcome", "result":
		outcome, err := normalizeRequestOutcome(value)
		if err != nil {
			return sortOrder, err
		}
		addFilterValue(&filters.Outcomes, outcome)
	case "status", "http", "http_status", "http-status":
		if isHTTPClass(value) {
			addFilterValue(&filters.HTTPClasses, strings.ToLower(value))
			return sortOrder, nil
		}
		if _, err := strconv.Atoi(value); err != nil {
			return sortOrder, fmt.Errorf("status filter requires a status code or class")
		}
		addFilterValue(&filters.HTTPStatuses, value)
	case "error", "error_category", "error-category":
		addFilterValue(&filters.ErrorCategories, value)
	case "cache":
		cacheState, err := normalizeCacheState(value)
		if err != nil {
			return sortOrder, err
		}
		addFilterValue(&filters.CacheStates, cacheState)
	case "id", "request", "request_id", "request-id":
		filters.RequestIDSubstr = value
	case "hidden", "chart":
		respect, err := normalizeChartVisibilityFilter(value)
		if err != nil {
			return sortOrder, err
		}
		filters.RespectChartVisibility = respect
		filters.ChartVisibilitySet = true
	case "sort":
		parsed, err := parseRequestSort(value)
		if err != nil {
			return sortOrder, err
		}
		sortOrder = parsed
	default:
		return sortOrder, fmt.Errorf("unknown request filter key %q", key)
	}
	return sortOrder, nil
}

func parseMetricRangeToken(token string) (metricRangeFilter, bool, error) {
	for _, op := range []string{">=", "<=", ">", "<", "="} {
		key, value, ok := strings.Cut(token, op)
		if !ok {
			continue
		}
		name, err := normalizeMetricFilterName(key)
		if err != nil {
			return metricRangeFilter{}, true, err
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return metricRangeFilter{}, true, errors.New("metric filter requires a numeric value")
		}
		return metricRangeFilter{Name: name, Op: op, Value: parsed}, true, nil
	}
	return metricRangeFilter{}, false, nil
}

func (filters requestFilters) withDefaultVisibility() requestFilters {
	if !filters.RespectChartVisibility && !filters.ChartVisibilitySet {
		filters.RespectChartVisibility = true
	}
	return filters
}

func (filters requestFilters) isEmpty() bool {
	return filters.isEmptyExceptVisibility() && !filters.ChartVisibilitySet
}

func (filters requestFilters) isEmptyExceptVisibility() bool {
	return filters.Query == "" &&
		len(filters.TargetIDs) == 0 &&
		len(filters.Models) == 0 &&
		len(filters.ProviderAPIs) == 0 &&
		len(filters.Phases) == 0 &&
		len(filters.Outcomes) == 0 &&
		len(filters.HTTPStatuses) == 0 &&
		len(filters.HTTPClasses) == 0 &&
		len(filters.ErrorCategories) == 0 &&
		len(filters.CacheStates) == 0 &&
		len(filters.MetricRanges) == 0 &&
		filters.RequestIDSubstr == ""
}

func (filters requestFilters) matches(row requestRow) bool {
	if filters.RequestIDSubstr != "" && !containsFold(row.RequestID, filters.RequestIDSubstr) {
		return false
	}
	if filters.Query != "" && !requestRowMatchesText(row, filters.Query) {
		return false
	}
	if !matchesFilterMap(filters.TargetIDs, row.TargetID) {
		return false
	}
	if !matchesFilterMap(filters.Models, row.Model) {
		return false
	}
	if !matchesFilterMap(filters.ProviderAPIs, row.ProviderAPI) {
		return false
	}
	if !matchesFilterMap(filters.Phases, row.Phase) {
		return false
	}
	if !matchesFilterMap(filters.Outcomes, row.Outcome) {
		return false
	}
	if !matchesFilterMap(filters.HTTPStatuses, row.HTTPStatus) {
		return false
	}
	if len(filters.HTTPClasses) > 0 && !filters.HTTPClasses[requestHTTPClass(row.HTTPStatus)] {
		return false
	}
	if !matchesFilterMap(filters.ErrorCategories, row.ErrorCategory) {
		return false
	}
	if !matchesFilterMap(filters.CacheStates, row.CacheState) {
		return false
	}
	for _, metric := range filters.MetricRanges {
		if !metric.matches(row) {
			return false
		}
	}
	return true
}

func (metric metricRangeFilter) matches(row requestRow) bool {
	value := requestRowMetricValue(row, metric.Name)
	if value == nil {
		return false
	}
	switch metric.Op {
	case ">":
		return *value > metric.Value
	case ">=":
		return *value >= metric.Value
	case "<":
		return *value < metric.Value
	case "<=":
		return *value <= metric.Value
	case "=":
		return *value == metric.Value
	default:
		return false
	}
}

func requestRowMetricValue(row requestRow, name string) *float64 {
	switch name {
	case metricTTFTDeltaMS:
		return row.TTFTMS
	case metricE2EDeltaMS:
		return row.E2EMS
	case metricStreamTotalMS:
		return row.StreamTotalMS
	case metricHTTPTTFBMS:
		return row.TTFBMS
	case metricE2EOutputTPS:
		return row.E2EOutputTPS
	case metricGenerationDeltaOutputTPS:
		return row.GenerationTPS
	case metricCompletionTokens:
		if row.CompletionTokens == nil {
			return nil
		}
		value := float64(*row.CompletionTokens)
		return &value
	case "prompt_tokens":
		if row.PromptTokens == nil {
			return nil
		}
		value := float64(*row.PromptTokens)
		return &value
	case "total_tokens":
		if row.TotalTokens == nil {
			return nil
		}
		value := float64(*row.TotalTokens)
		return &value
	default:
		return nil
	}
}

func requestRowMatchesText(row requestRow, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	fields := []string{
		row.RequestID,
		row.TargetID,
		row.TargetName,
		row.Provider,
		row.ProviderAPI,
		row.Model,
		row.ServiceTier,
		row.Phase,
		row.Outcome,
		row.HTTPStatus,
		row.ErrorCategory,
		row.FinishReason,
		row.CacheState,
		row.Conn,
		row.Protocol,
		row.OutputState,
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func renderRequestFilterQuery(filters requestFilters, sortOrder requestSort) string {
	var tokens []string
	tokens = appendFilterMapTokens(tokens, "target", filters.TargetIDs)
	tokens = appendFilterMapTokens(tokens, "model", filters.Models)
	tokens = appendFilterMapTokens(tokens, "api", filters.ProviderAPIs)
	tokens = appendFilterMapTokens(tokens, "phase", filters.Phases)
	tokens = appendFilterMapTokens(tokens, "outcome", filters.Outcomes)
	tokens = appendFilterMapTokens(tokens, "status", filters.HTTPStatuses)
	tokens = appendFilterMapTokens(tokens, "status", filters.HTTPClasses)
	tokens = appendFilterMapTokens(tokens, "error", filters.ErrorCategories)
	tokens = appendFilterMapTokens(tokens, "cache", filters.CacheStates)
	if filters.RequestIDSubstr != "" {
		tokens = append(tokens, "id:"+filters.RequestIDSubstr)
	}
	if filters.Query != "" {
		tokens = append(tokens, filters.Query)
	}
	for _, metric := range filters.MetricRanges {
		tokens = append(tokens, metricToken(metric))
	}
	if filters.ChartVisibilitySet && !filters.RespectChartVisibility {
		tokens = append(tokens, "hidden:all")
	}
	if sortOrder == "" {
		sortOrder = requestSortCompletionOrder
	}
	if sortOrder != requestSortCompletionOrder {
		tokens = append(tokens, "sort:"+sortToken(sortOrder))
	}
	return strings.Join(tokens, " ")
}

func appendFilterMapTokens(tokens []string, key string, values map[string]bool) []string {
	keys := sortedFilterKeys(values)
	for _, value := range keys {
		tokens = append(tokens, key+":"+value)
	}
	return tokens
}

func sortedFilterKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, enabled := range values {
		if enabled {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func metricToken(metric metricRangeFilter) string {
	return metric.Name + metric.Op + strconv.FormatFloat(metric.Value, 'f', -1, 64)
}

func sortToken(sortOrder requestSort) string {
	switch sortOrder {
	case requestSortSlowestTTFT:
		return "-ttft"
	case requestSortSlowestE2E:
		return "-e2e"
	case requestSortSlowestStream:
		return "-stream"
	case requestSortHighestTPS:
		return "-tps"
	case requestSortLowestTPS:
		return "tps"
	case requestSortErrorsFirst:
		return "errors"
	case requestSortTargetOrder:
		return "target"
	default:
		return "completion"
	}
}

func addFilterValue(target *map[string]bool, value string) {
	if *target == nil {
		*target = make(map[string]bool)
	}
	(*target)[strings.ToLower(strings.TrimSpace(value))] = true
}

func matchesFilterMap(values map[string]bool, value string) bool {
	if len(values) == 0 {
		return true
	}
	return values[strings.ToLower(value)]
}

func normalizeRequestPhase(value string) (string, error) {
	switch strings.ToLower(value) {
	case requestPhaseMeasured, "measure", "meas", "false":
		return requestPhaseMeasured, nil
	case requestPhaseWarmup, "warm", "true":
		return requestPhaseWarmup, nil
	default:
		return "", fmt.Errorf("unknown phase filter %q", value)
	}
}

func phaseFromWarmupFilter(value string) (string, error) {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "y", requestPhaseWarmup:
		return requestPhaseWarmup, nil
	case "false", "0", "no", "n", requestPhaseMeasured:
		return requestPhaseMeasured, nil
	default:
		return "", fmt.Errorf("warmup filter requires true or false")
	}
}

func normalizeRequestOutcome(value string) (string, error) {
	switch strings.ToLower(value) {
	case requestOutcomeOK, "success", "succeeded", "successful":
		return requestOutcomeOK, nil
	case requestOutcomeError, "err", "failed", "fail", "failure":
		return requestOutcomeError, nil
	default:
		return "", fmt.Errorf("unknown outcome filter %q", value)
	}
}

func normalizeCacheState(value string) (string, error) {
	switch strings.ToLower(value) {
	case requestCacheHit, requestCacheMiss, requestCacheUnknown:
		return strings.ToLower(value), nil
	default:
		return "", fmt.Errorf("unknown cache filter %q", value)
	}
}

func normalizeChartVisibilityFilter(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "visible", "on", "true", "yes", "respect":
		return true, nil
	case "all", "off", "false", "no", "ignore":
		return false, nil
	default:
		return true, fmt.Errorf("hidden filter requires all or visible")
	}
}

func normalizeMetricFilterName(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ttft", metricTTFTDeltaMS:
		return metricTTFTDeltaMS, nil
	case "e2e", metricE2EDeltaMS:
		return metricE2EDeltaMS, nil
	case "stream", metricStreamTotalMS:
		return metricStreamTotalMS, nil
	case "ttfb", metricHTTPTTFBMS:
		return metricHTTPTTFBMS, nil
	case "tps", metricE2EOutputTPS:
		return metricE2EOutputTPS, nil
	case "generation_tps", metricGenerationDeltaOutputTPS:
		return metricGenerationDeltaOutputTPS, nil
	case "tokens", metricCompletionTokens:
		return metricCompletionTokens, nil
	case "prompt_tokens":
		return "prompt_tokens", nil
	case "total_tokens":
		return "total_tokens", nil
	default:
		return "", fmt.Errorf("unknown metric filter %q", value)
	}
}

func normalizedRequestSort(sortOrder requestSort) requestSort {
	if sortOrder == "" {
		return requestSortCompletionOrder
	}
	return sortOrder
}

func requestFilterDisplay(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return requestDetailRedacted(value)
}

func parseRequestSort(value string) (requestSort, error) {
	switch strings.ToLower(value) {
	case "completion", "completion-order", "order", "default":
		return requestSortCompletionOrder, nil
	case "ttft", "-ttft", "slowest-ttft":
		return requestSortSlowestTTFT, nil
	case "e2e", "-e2e", "slowest-e2e":
		return requestSortSlowestE2E, nil
	case "stream", "-stream", "slowest-stream":
		return requestSortSlowestStream, nil
	case "tps", "-tps", "highest-tps":
		return requestSortHighestTPS, nil
	case "+tps", "lowest-tps":
		return requestSortLowestTPS, nil
	case "errors", "error", "errors-first", "status":
		return requestSortErrorsFirst, nil
	case "target", "model", "target-order":
		return requestSortTargetOrder, nil
	default:
		return "", fmt.Errorf("unknown sort %q", value)
	}
}

func requestHTTPClass(status string) string {
	if len(status) != 3 {
		return "-"
	}
	if status[0] < '1' || status[0] > '5' {
		return "-"
	}
	return string(status[0]) + "xx"
}

func isHTTPClass(value string) bool {
	value = strings.ToLower(value)
	return len(value) == 3 && value[1:] == "xx" && value[0] >= '1' && value[0] <= '5'
}

func containsFold(value string, substr string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(substr))
}
