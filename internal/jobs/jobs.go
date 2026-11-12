// Package jobs holds the worker loop helpers (SPEC §7): claim → heartbeat →
// run → finish, with lease expiry handled by the store's claim semantics
// (expired running jobs become `unknown`, never silently requeued).
package jobs

import (
	"context"
	"time"

	"github.com/aravinditte/benri-agent/internal/store"
)

const leaseMS = 120_000

// ProcessQueuedRuns claims and runs up to max agent runs via the handler.
// The handler (pipeline) is resume-safe via checkpoints.
func ProcessQueuedRuns(ctx context.Context, st store.Repository, handler func(ctx context.Context, runID string) error, max int) (int, error) {
	processed := 0
	for processed < max {
		select {
		case <-ctx.Done():
			return processed, ctx.Err()
		default:
		}
		run, err := st.ClaimNextRun("agent-worker", leaseMS)
		if err != nil {
			return processed, err
		}
		if run == nil {
			return processed, nil
		}
		stop := startHeartbeat(ctx, func() { _ = st.HeartbeatRun(run.ID, leaseMS) })
		err = handler(ctx, run.ID)
		stop()
		if err != nil {
			if _, ok := err.(*agentCrash); ok {
				// Simulated/real crash: leave the run for lease-expiry/resume.
				processed++
				continue
			}
			code := "WORKER_ERROR"
			_ = st.FinishRun(run.ID, store.FinishRunPatch{State: "failed", FailureCode: &code})
		}
		processed++
	}
	return processed, nil
}

// agentCrash marks errors after which the run must NOT be marked failed
// (the worker died mid-run; a later worker resumes from checkpoints).
type agentCrash struct{ Err error }

func (e *agentCrash) Error() string { return e.Err.Error() }

// WrapCrash wraps a crash-simulation error for ProcessQueuedRuns.
func WrapCrash(err error) error {
	if err == nil {
		return nil
	}
	return &agentCrash{Err: err}
}

// IsCrash reports whether err is a wrapped crash.
func IsCrash(err error) bool {
	_, ok := err.(*agentCrash)
	return ok
}

// ProcessQueuedJobs claims and executes up to max execution jobs. Jobs the
// claim step marks `unknown` (expired lease) are counted but not executed.
func ProcessQueuedJobs(ctx context.Context, st store.Repository, handler func(ctx context.Context, job *store.ExecutionJob) error, max int) (int, error) {
	processed := 0
	for processed < max {
		select {
		case <-ctx.Done():
			return processed, ctx.Err()
		default:
		}
		job, err := st.ClaimNextJob("executor-worker", leaseMS)
		if err != nil {
			return processed, err
		}
		if job == nil {
			return processed, nil
		}
		processed++
		if job.State != "running" {
			continue // expired-lease job already marked unknown by the claim
		}
		stop := startHeartbeat(ctx, func() { _ = st.HeartbeatJob(job.ID, leaseMS) })
		_ = handler(ctx, job)
		stop()
	}
	return processed, nil
}

func startHeartbeat(ctx context.Context, beat func()) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				beat()
			}
		}
	}()
	return func() { close(done) }
}
