package whatttft

import (
	"context"
	"sort"
	"sync"
)

// interleavedJobSpec describes one request to run for a target, before a recorder is created.
type interleavedJobSpec struct {
	targetIndex int
	runner      *Runner
	cfg         RunConfig
	attempt     int
	warmup      bool
}

// interleavedJob is an interleavedJobSpec with its per-request recorder attached at enqueue time.
type interleavedJob struct {
	interleavedJobSpec
	recorder *Recorder
}

// interleavedOutput is one completed interleaved request result.
type interleavedOutput struct {
	targetIndex int
	attempt     int
	record      RequestRecord
	chunks      []ChunkRecord
}

// runInterleaved executes every target together under a single shared concurrency budget, running
// the warmup phase across all targets before the measured phase. Requests are interleaved
// round-robin across targets so each target progresses in the same time window; the shared budget
// bounds total in-flight requests to the configured concurrency rather than per-target concurrency.
func (r *BenchmarkRunner) runInterleaved(ctx context.Context, targets []normalizedBenchmarkTarget, result *BenchmarkResult) error {
	runners := make([]*Runner, len(targets))
	for index, target := range targets {
		runners[index] = newRunnerWithEmitter(target.provider, target.config, r.events)
		r.emit(ctx, benchmarkTargetEvent(EventTargetStarted, r.cfg.Name, target, result))
	}

	budget := interleavedBudget(targets)

	if err := r.runInterleavedPhase(ctx, targets, runners, result, true, budget); err != nil {
		return err
	}
	if err := r.runInterleavedPhase(ctx, targets, runners, result, false, budget); err != nil {
		return err
	}

	for _, target := range targets {
		r.emit(ctx, benchmarkTargetEvent(EventTargetFinished, r.cfg.Name, target, result))
	}

	return nil
}

// interleavedBudget is the shared worker count: the maximum per-target concurrency, which equals
// the run-level concurrency when every target inherits the same value.
func interleavedBudget(targets []normalizedBenchmarkTarget) int {
	budget := 1
	for _, target := range targets {
		if target.config.Concurrency > budget {
			budget = target.config.Concurrency
		}
	}

	return budget
}

// interleavedPhaseCount returns the request count and starting attempt index for a target's phase.
func interleavedPhaseCount(cfg RunConfig, warmup bool) (count int, startAttempt int) {
	if warmup {
		return cfg.WarmupRequests, 0
	}

	return cfg.MeasuredRequests, cfg.WarmupRequests
}

// buildInterleavedSpecs builds the round-robin job order for a phase: one request from each target
// in turn, then the next from each, so a shared worker pool spreads its first picks across targets.
func buildInterleavedSpecs(targets []normalizedBenchmarkTarget, runners []*Runner, warmup bool) []interleavedJobSpec {
	maxCount := 0
	for _, target := range targets {
		count, _ := interleavedPhaseCount(target.config, warmup)
		if count > maxCount {
			maxCount = count
		}
	}

	specs := make([]interleavedJobSpec, 0)
	for offset := range maxCount {
		for index, target := range targets {
			count, startAttempt := interleavedPhaseCount(target.config, warmup)
			if offset >= count {
				continue
			}
			specs = append(specs, interleavedJobSpec{
				targetIndex: index,
				runner:      runners[index],
				cfg:         target.config,
				attempt:     startAttempt + offset,
				warmup:      warmup,
			})
		}
	}

	return specs
}

func (r *BenchmarkRunner) runInterleavedPhase(
	ctx context.Context,
	targets []normalizedBenchmarkTarget,
	runners []*Runner,
	result *BenchmarkResult,
	warmup bool,
	budget int,
) error {
	specs := buildInterleavedSpecs(targets, runners, warmup)
	if len(specs) == 0 {
		return nil
	}

	// Phase events carry no completed count or summary: result is the benchmark-wide aggregate, and
	// passing it here would make every target's per-target counters adopt the global totals (which
	// the dashboard then sums across targets, double-counting). Per-target progress is derived from
	// request records instead.
	for index, target := range targets {
		if count, _ := interleavedPhaseCount(target.config, warmup); count > 0 {
			runners[index].emitPhaseEvent(ctx, target.config, EventPhaseStarted, warmup, 0, nil)
		}
	}

	outputs, phaseErr := r.runInterleavedPool(ctx, specs, budget)
	for _, output := range outputs {
		result.Records = append(result.Records, output.record)
		result.Chunks = append(result.Chunks, output.chunks...)
	}
	result.Summary = Summarize(result.Records)

	for index, target := range targets {
		if count, _ := interleavedPhaseCount(target.config, warmup); count > 0 {
			runners[index].emitPhaseEvent(ctx, target.config, EventPhaseFinished, warmup, 0, nil)
		}
	}

	return phaseErr
}

func (r *BenchmarkRunner) runInterleavedPool(ctx context.Context, specs []interleavedJobSpec, budget int) ([]interleavedOutput, error) {
	workerCount := budget
	if workerCount > len(specs) {
		workerCount = len(specs)
	}

	jobs := make(chan interleavedJob)
	outputs := make(chan interleavedOutput, len(specs))

	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				record, chunks := job.runner.runOne(ctx, job.cfg, job.attempt, job.warmup, job.recorder)
				job.runner.emitRequestFinished(ctx, job.cfg, record, 0, nil)
				outputs <- interleavedOutput{targetIndex: job.targetIndex, attempt: job.attempt, record: record, chunks: chunks}
			}
		}()
	}

	enqueueErr := r.enqueueInterleavedJobs(ctx, jobs, specs)
	close(jobs)
	wg.Wait()
	close(outputs)

	collected := collectInterleavedOutputs(outputs)
	if enqueueErr != nil {
		return collected, enqueueErr
	}
	if err := ctx.Err(); err != nil {
		return collected, err
	}

	return collected, nil
}

func (r *BenchmarkRunner) enqueueInterleavedJobs(ctx context.Context, jobs chan<- interleavedJob, specs []interleavedJobSpec) error {
	for _, spec := range specs {
		if err := ctx.Err(); err != nil {
			return err
		}

		job := interleavedJob{interleavedJobSpec: spec, recorder: newScheduledRecorder()}
		spec.runner.emitRequestScheduled(ctx, spec.cfg, spec.attempt, spec.warmup)

		select {
		case jobs <- job:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// collectInterleavedOutputs drains outputs and orders them by target then attempt so combined
// records are deterministic regardless of completion order.
func collectInterleavedOutputs(outputs <-chan interleavedOutput) []interleavedOutput {
	collected := make([]interleavedOutput, 0)
	for output := range outputs {
		collected = append(collected, output)
	}

	sort.SliceStable(collected, func(i int, j int) bool {
		if collected[i].targetIndex != collected[j].targetIndex {
			return collected[i].targetIndex < collected[j].targetIndex
		}

		return collected[i].attempt < collected[j].attempt
	})

	return collected
}
