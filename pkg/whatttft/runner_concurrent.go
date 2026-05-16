package whatttft

import (
	"context"
	"sort"
	"sync"
)

type runnerJob struct {
	attempt  int
	warmup   bool
	recorder *Recorder
}

type runnerOutput struct {
	attempt int
	record  RequestRecord
	chunks  []ChunkRecord
}

func (r *Runner) runConcurrent(ctx context.Context, cfg RunConfig) (*RunResult, error) {
	total := cfg.WarmupRequests + cfg.MeasuredRequests
	result := newRunResult(total, cfg.SaveChunks)

	if err := r.appendConcurrentPhase(ctx, cfg, result, 0, cfg.WarmupRequests, true); err != nil {
		result.Summary = Summarize(result.Records)
		return result, err
	}

	if err := r.appendConcurrentPhase(ctx, cfg, result, cfg.WarmupRequests, cfg.MeasuredRequests, false); err != nil {
		result.Summary = Summarize(result.Records)
		return result, err
	}

	result.Summary = Summarize(result.Records)
	return result, nil
}

func (r *Runner) appendConcurrentPhase(
	ctx context.Context,
	cfg RunConfig,
	result *RunResult,
	startAttempt int,
	count int,
	warmup bool,
) error {
	outputs, err := r.runConcurrentPhase(ctx, cfg, startAttempt, count, warmup)
	for _, output := range outputs {
		appendRunOutput(result, output.record, output.chunks, cfg.SaveChunks)
	}

	return err
}

func (r *Runner) runConcurrentPhase(
	ctx context.Context,
	cfg RunConfig,
	startAttempt int,
	count int,
	warmup bool,
) ([]runnerOutput, error) {
	if count == 0 {
		return nil, nil
	}

	workerCount := cfg.Concurrency
	if workerCount > count {
		workerCount = count
	}

	jobs := make(chan runnerJob)
	outputs := make(chan runnerOutput, count)

	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				record, chunks := r.runOne(ctx, cfg, job.attempt, job.warmup, job.recorder)
				outputs <- runnerOutput{attempt: job.attempt, record: record, chunks: chunks}
			}
		}()
	}

	enqueueErr := enqueueRunnerJobs(ctx, jobs, startAttempt, count, warmup)
	close(jobs)
	wg.Wait()
	close(outputs)

	collected := collectRunnerOutputs(outputs)
	if enqueueErr != nil {
		return collected, enqueueErr
	}
	if err := ctx.Err(); err != nil {
		return collected, err
	}

	return collected, nil
}

func enqueueRunnerJobs(ctx context.Context, jobs chan<- runnerJob, startAttempt int, count int, warmup bool) error {
	for offset := range count {
		if err := ctx.Err(); err != nil {
			return err
		}

		job := runnerJob{
			attempt:  startAttempt + offset,
			warmup:   warmup,
			recorder: newScheduledRecorder(),
		}

		select {
		case jobs <- job:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func collectRunnerOutputs(outputs <-chan runnerOutput) []runnerOutput {
	collected := make([]runnerOutput, 0)
	for output := range outputs {
		collected = append(collected, output)
	}

	sort.Slice(collected, func(i int, j int) bool {
		return collected[i].attempt < collected[j].attempt
	})

	return collected
}
