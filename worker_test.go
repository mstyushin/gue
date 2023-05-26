package gue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/mstyushin/gue/v4/adapter"
	adapterTesting "github.com/mstyushin/gue/v4/adapter/testing"
)

type mockHook struct {
	called int
	ctx    context.Context
	j      *Job
	err    error
}

func (h *mockHook) handler(ctx context.Context, j *Job, err error) {
	h.called++
	h.ctx, h.j, h.err = ctx, j, err
}

func TestWorkerWorkOne(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testWorkerWorkOne(t, openFunc(t))
		})
	}
}

func testWorkerWorkOne(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	var success bool
	wm := WorkMap{
		"MyJob": func(ctx context.Context, j *Job) error {
			success = true
			return nil
		},
	}

	jobLockedHook := new(mockHook)
	unknownJobTypeHook := new(mockHook)
	jobDoneHook := new(mockHook)

	w, err := NewWorker(
		c,
		wm,
		WithWorkerHooksJobLocked(jobLockedHook.handler),
		WithWorkerHooksUnknownJobType(unknownJobTypeHook.handler),
		WithWorkerHooksJobDone(jobDoneHook.handler),
	)
	require.NoError(t, err)

	didWork := w.WorkOne(ctx)
	assert.False(t, didWork)

	err = c.Enqueue(ctx, &Job{Type: "MyJob"})
	require.NoError(t, err)

	didWork = w.WorkOne(ctx)
	assert.True(t, didWork)
	assert.True(t, success)

	assert.Equal(t, 1, jobLockedHook.called)
	assert.NotNil(t, jobLockedHook.j)
	assert.NoError(t, jobLockedHook.err)

	assert.Equal(t, 0, unknownJobTypeHook.called)

	assert.Equal(t, 1, jobDoneHook.called)
	assert.NotNil(t, jobDoneHook.j)
	assert.NoError(t, jobDoneHook.err)
}

func TestWorker_Run(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testWorkerRun(t, openFunc(t))
		})
	}
}

func testWorkerRun(t *testing.T, connPool adapter.ConnPool) {
	c, err := NewClient(connPool)
	require.NoError(t, err)

	w, err := NewWorker(c, WorkMap{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	var grp errgroup.Group
	grp.Go(func() error {
		return w.Run(ctx)
	})

	// give worker time to start
	time.Sleep(time.Second)

	assert.True(t, w.running)

	// try to start one more time to get an error about already running worker
	assert.Error(t, w.Run(context.Background()))

	cancel()
	assert.NoError(t, grp.Wait())

	assert.False(t, w.running)
}

func TestWorkerPool_Run(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testWorkerPoolRun(t, openFunc(t))
		})
	}
}

func testWorkerPoolRun(t *testing.T, connPool adapter.ConnPool) {
	c, err := NewClient(connPool)
	require.NoError(t, err)

	poolSize := 2
	w, err := NewWorkerPool(c, WorkMap{}, poolSize)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	var grp errgroup.Group
	grp.Go(func() error {
		return w.Run(ctx)
	})

	// give worker time to start
	time.Sleep(time.Second)

	assert.True(t, w.running)
	for i := range w.workers {
		assert.True(t, w.workers[i].running)
	}

	// try to start one more time to get an error about already running worker pool
	assert.Error(t, w.Run(context.Background()))

	cancel()

	require.NoError(t, grp.Wait())

	assert.False(t, w.running)
	for i := range w.workers {
		assert.False(t, w.workers[i].running)
	}
}

func TestWorkerPool_WorkOne(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testWorkerPoolWorkOne(t, openFunc(t))
		})
	}
}

func testWorkerPoolWorkOne(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	var success bool
	wm := WorkMap{
		"MyJob": func(ctx context.Context, j *Job) error {
			success = true
			return nil
		},
	}

	jobLockedHook := new(mockHook)
	unknownJobTypeHook := new(mockHook)
	jobDoneHook := new(mockHook)

	w, err := NewWorkerPool(
		c,
		wm,
		3,
		WithPoolHooksJobLocked(jobLockedHook.handler),
		WithPoolHooksUnknownJobType(unknownJobTypeHook.handler),
		WithPoolHooksJobDone(jobDoneHook.handler),
	)
	require.NoError(t, err)

	didWork := w.WorkOne(ctx)
	assert.False(t, didWork)

	err = c.Enqueue(ctx, &Job{Type: "MyJob"})
	require.NoError(t, err)

	didWork = w.WorkOne(ctx)
	assert.True(t, didWork)
	assert.True(t, success)

	assert.Equal(t, 1, jobLockedHook.called)
	assert.NotNil(t, jobLockedHook.j)
	assert.NoError(t, jobLockedHook.err)

	assert.Equal(t, 0, unknownJobTypeHook.called)

	assert.Equal(t, 1, jobDoneHook.called)
	assert.NotNil(t, jobDoneHook.j)
	assert.NoError(t, jobDoneHook.err)
}

func BenchmarkWorker(b *testing.B) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		b.Run(name, func(b *testing.B) {
			benchmarkWorker(b, openFunc(b))
		})
	}
}

func benchmarkWorker(b *testing.B, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(b, err)

	w, err := NewWorker(c, WorkMap{"Nil": nilWorker})
	require.NoError(b, err)

	for i := 0; i < b.N; i++ {
		if err := c.Enqueue(ctx, &Job{Type: "Nil"}); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.WorkOne(ctx)
	}
}

func nilWorker(context.Context, *Job) error {
	return nil
}

func TestWorkerWorkReturnsError(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testWorkerWorkReturnsError(t, openFunc(t))
		})
	}
}

func testWorkerWorkReturnsError(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	called := 0
	wm := WorkMap{
		"MyJob": func(ctx context.Context, j *Job) error {
			called++
			return errors.New("the error msg")
		},
	}

	jobLockedHook := new(mockHook)
	unknownJobTypeHook := new(mockHook)
	jobDoneHook := new(mockHook)

	w, err := NewWorker(
		c,
		wm,
		WithWorkerHooksJobLocked(jobLockedHook.handler),
		WithWorkerHooksUnknownJobType(unknownJobTypeHook.handler),
		WithWorkerHooksJobDone(jobDoneHook.handler),
	)
	require.NoError(t, err)

	didWork := w.WorkOne(ctx)
	assert.False(t, didWork)

	job := Job{Type: "MyJob"}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	didWork = w.WorkOne(ctx)
	assert.True(t, didWork)
	assert.Equal(t, 1, called)

	assert.Equal(t, 1, jobLockedHook.called)
	assert.NotNil(t, jobLockedHook.j)
	assert.NoError(t, jobLockedHook.err)

	assert.Equal(t, 0, unknownJobTypeHook.called)

	assert.Equal(t, 1, jobDoneHook.called)
	assert.NotNil(t, jobDoneHook.j)
	assert.Error(t, jobDoneHook.err)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	assert.Equal(t, int32(1), j.ErrorCount)
	assert.NotEqual(t, pgtype.Null, j.LastError.Status)
	assert.Equal(t, "the error msg", j.LastError.String)
}

func TestWorkerWorkRescuesPanic(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testWorkerWorkRescuesPanic(t, openFunc(t))
		})
	}
}

func testWorkerWorkRescuesPanic(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	called := 0
	wm := WorkMap{
		"MyJob": func(ctx context.Context, j *Job) error {
			called++
			panic("the panic msg")
		},
	}
	w, err := NewWorker(c, wm)
	require.NoError(t, err)

	job := Job{Type: "MyJob"}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	w.WorkOne(ctx)
	assert.Equal(t, 1, called)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	assert.Equal(t, int32(1), j.ErrorCount)
	assert.NotEqual(t, pgtype.Null, j.LastError.Status)
	assert.Contains(t, j.LastError.String, "the panic msg\n")
	// basic check if a stacktrace is there - not the stacktrace format itself
	assert.Contains(t, j.LastError.String, "worker.go:")
	assert.Contains(t, j.LastError.String, "worker_test.go:")
}

func TestWorkerWorkOneTypeNotInMap(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testWorkerWorkOneTypeNotInMap(t, openFunc(t))
		})
	}
}

func testWorkerWorkOneTypeNotInMap(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	wm := WorkMap{}

	jobLockedHook := new(mockHook)
	unknownJobTypeHook := new(mockHook)
	jobDoneHook := new(mockHook)

	w, err := NewWorker(
		c,
		wm,
		WithWorkerHooksJobLocked(jobLockedHook.handler),
		WithWorkerHooksUnknownJobType(unknownJobTypeHook.handler),
		WithWorkerHooksJobDone(jobDoneHook.handler),
	)
	require.NoError(t, err)

	didWork := w.WorkOne(ctx)
	assert.False(t, didWork)

	assert.Equal(t, 0, jobLockedHook.called)
	assert.Equal(t, 0, unknownJobTypeHook.called)
	assert.Equal(t, 0, jobDoneHook.called)

	job := Job{Type: "MyJob"}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	didWork = w.WorkOne(ctx)
	assert.True(t, didWork)

	assert.Equal(t, 1, jobLockedHook.called)
	assert.NotNil(t, jobLockedHook.j)
	assert.NoError(t, jobLockedHook.err)

	assert.Equal(t, 1, unknownJobTypeHook.called)
	assert.NotNil(t, unknownJobTypeHook.j)
	assert.Error(t, unknownJobTypeHook.err)

	assert.Equal(t, 0, jobDoneHook.called)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	assert.Equal(t, int32(1), j.ErrorCount)
	require.NotEqual(t, pgtype.Null, j.LastError.Status)
	assert.Contains(t, j.LastError.String, `unknown job type: "MyJob"`)
}
