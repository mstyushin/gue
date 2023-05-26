package gue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgtype"

	"github.com/mstyushin/gue/v4/adapter"
)

// JobPriority is the wrapper type for Job.Priority
type JobPriority int16

// Some shortcut values for JobPriority that can be any, but chances are high that one of these will be the most used.
const (
	JobPriorityHighest JobPriority = -32768
	JobPriorityHigh    JobPriority = -16384
	JobPriorityDefault JobPriority = 0
	JobPriorityLow     JobPriority = 16384
	JobPriorityLowest  JobPriority = 32767
)

// Job is a single unit of work for Gue to perform.
type Job struct {
	// ID is the unique database ID of the Job. It is ignored on job creation.
	ID int64

	// Queue is the name of the queue. It defaults to the empty queue "".
	Queue string

	// Priority is the priority of the Job. The default priority is 0, and a
	// lower number means a higher priority.
	//
	// The highest priority is JobPriorityHighest, the lowest one is JobPriorityLowest
	Priority JobPriority

	// RunAt is the time that this job should be executed. It defaults to now(),
	// meaning the job will execute immediately. Set it to a value in the future
	// to delay a job's execution.
	RunAt time.Time

	// Type maps job to a worker func.
	Type string

	// Args must be the bytes of a valid JSON string
	Args []byte

	// ErrorCount is the number of times this job has attempted to run, but failed with an error.
	// It is ignored on job creation.
	// This field is initialised only when the Job is being retrieved from the DB and is not
	// being updated when the current Job run errored.
	ErrorCount int32

	// LastError is the error message or stack trace from the last time the job failed. It is ignored on job creation.
	// This field is initialised only when the Job is being retrieved from the DB and is not
	// being updated when the current Job run errored.
	LastError pgtype.Text

	mu      sync.Mutex
	deleted bool
	pool    adapter.ConnPool
	tx      adapter.Tx
	backoff Backoff
	logger  adapter.Logger
}

// Tx returns DB transaction that this job is locked to. You may use
// it as you please until you call Done(). At that point, this transaction
// will be committed. This function will return nil if the Job's
// transaction was closed with Done().
func (j *Job) Tx() adapter.Tx {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.tx
}

// Delete marks this job as complete by deleting it from the database.
//
// You must also later call Done() to return this job's database connection to
// the pool. If you got the job from the worker - it will take care of cleaning up the job and resources,
// no need to do this manually in a WorkFunc.
func (j *Job) Delete(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.deleted {
		return nil
	}

	_, err := j.tx.Exec(ctx, `DELETE FROM gue_jobs WHERE job_id = $1`, j.ID)
	if err != nil {
		return err
	}

	j.deleted = true
	return nil
}

// Done commits transaction that marks job as done. If you got the job from the worker - it will take care of
// cleaning up the job and resources, no need to do this manually in a WorkFunc.
func (j *Job) Done(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.tx == nil || j.pool == nil {
		// already marked as done
		return nil
	}

	if err := j.tx.Commit(ctx); err != nil {
		return err
	}

	j.pool = nil
	j.tx = nil

	return nil
}

// Error marks the job as failed and schedules it to be reworked. An error
// message or backtrace can be provided as msg, which will be saved on the job.
// It will also increase the error count.
//
// This call marks job as done and releases (commits) transaction,
// so calling Done() is not required, although calling it will not cause any issues.
// If you got the job from the worker - it will take care of cleaning up the job and resources,
// no need to do this manually in a WorkFunc.
func (j *Job) Error(ctx context.Context, msg string) (err error) {
	defer func() {
		doneErr := j.Done(ctx)
		if doneErr != nil {
			err = fmt.Errorf("failed to mark job as done (original error: %v): %w", err, doneErr)
		}
	}()

	errorCount := j.ErrorCount + 1
	backoff := j.backoff(int(errorCount))
	if backoff < 0 {
		j.logger.Info(
			"Got negative backoff for a job, discarding it",
			adapter.F("backoff", backoff),
			adapter.F("job-type", j.Type),
			adapter.F("job-queue", j.Queue),
			adapter.F("job-errors", errorCount),
		)
		err = j.Delete(ctx)
		return
	}

	now := time.Now().UTC()
	newRunAt := now.Add(backoff)

	_, err = j.tx.Exec(
		ctx,
		`UPDATE gue_jobs SET error_count = $1, run_at = $2, last_error = $3, updated_at = $4 WHERE job_id = $5`,
		errorCount, newRunAt, msg, now, j.ID,
	)

	return err
}
