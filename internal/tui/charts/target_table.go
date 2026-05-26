package charts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gabrielmbmb/what-ttft/pkg/whatttft"
)

// TargetTable renders a stable target comparison table for summary groups.
func TargetTable(groups []whatttft.SummaryGroup, width int) string {
	if len(groups) == 0 {
		return "target comparison\n(no target groups)"
	}
	groups = append([]whatttft.SummaryGroup(nil), groups...)
	sort.Slice(groups, func(i int, j int) bool {
		return targetSortKey(groups[i]) < targetSortKey(groups[j])
	})

	modelWidth := 16
	if width > 100 {
		modelWidth = 24
	}

	var builder strings.Builder
	builder.WriteString("target comparison\n")
	fmt.Fprintf(&builder, "%-18s %-*s %4s %4s %9s %9s %15s %15s %9s %10s %8s", "target", modelWidth, "model", "ok", "err", "ttft_p50", "e2e_p50", "e2e_output_tps", "gen_delta_tps", "gen_count", "system", "rps")
	for _, group := range groups {
		builder.WriteByte('\n')
		fmt.Fprintf(
			&builder,
			"%-18s %-*s %4d %4d %9s %9s %15s %15s %9s %10s %8s",
			truncate(groupLabel(group), 18),
			modelWidth,
			truncate(group.Model, modelWidth),
			group.SuccessfulRequests,
			group.ErrorRequests,
			formatOptional(group.Metrics.TTFTDeltaMS.P50),
			formatOptional(group.Metrics.E2EDeltaMS.P50),
			formatOptional(group.Metrics.E2EOutputTPS.Mean),
			formatOptional(group.Metrics.GenerationDeltaOutputTPS.Mean),
			formatCount(group.Metrics.GenerationDeltaOutputTPS.Count, group.SuccessfulRequests),
			formatOptional(group.SystemTPS),
			formatOptional(group.RPS),
		)
	}

	return builder.String()
}

func formatCount(count int, denominator int) string {
	if denominator <= 0 {
		return "0/0"
	}

	return fmt.Sprintf("%d/%d", count, denominator)
}

func targetSortKey(group whatttft.SummaryGroup) string {
	return group.TargetID + "\x00" + group.Provider + "\x00" + group.Model + "\x00" + group.ScenarioName
}
