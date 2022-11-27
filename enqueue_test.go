package gue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vgarvardt/gue/v5/adapter"
	adapterTesting "github.com/vgarvardt/gue/v5/adapter/testing"
)

func TestEnqueueOnlyType(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testEnqueueOnlyType(t, openFunc(t))
		})
	}
}

func testEnqueueOnlyType(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	jobType := "MyJob"
	job := Job{Type: jobType}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	// check resulting job
	assert.NotEmpty(t, j.ID)
	assert.Equal(t, defaultQueueName, j.Queue)
	assert.Equal(t, JobPriorityDefault, j.Priority)
	assert.False(t, j.RunAt.IsZero())
	assert.Equal(t, jobType, j.Type)
	assert.Equal(t, []byte(``), j.Args)
	assert.Equal(t, int32(0), j.ErrorCount)
	assert.False(t, j.LastError.Valid)
}

func TestEnqueueWithPriority(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testEnqueueWithPriority(t, openFunc(t))
		})
	}
}

func testEnqueueWithPriority(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	want := JobPriority(99)
	job := Job{Type: "MyJob", Priority: want}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	assert.Equal(t, want, j.Priority)
}

func TestEnqueueWithRunAt(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testEnqueueWithRunAt(t, openFunc(t))
		})
	}
}

func testEnqueueWithRunAt(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	want := time.Now().Add(2 * time.Minute)
	job := Job{Type: "MyJob", RunAt: want}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	// truncate to the microsecond as postgres driver does
	// UPD: truncate to the second as MySQL rounds ms: "22:59:36.553528" -> "21:59:37"
	assert.WithinDuration(t, want, j.RunAt, time.Second)
}

func TestEnqueueWithArgs(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testEnqueueWithArgs(t, openFunc(t))
		})
	}
}

func testEnqueueWithArgs(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	want := []byte(`{"arg1":0, "arg2":"a string"}`)
	job := Job{Type: "MyJob", Args: want}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	assert.Equal(t, want, j.Args)
}

func TestEnqueueWithQueue(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testEnqueueWithQueue(t, openFunc(t))
		})
	}
}

func testEnqueueWithQueue(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	want := "special-work-queue"
	job := Job{Type: "MyJob", Queue: want}
	err = c.Enqueue(ctx, &job)
	require.NoError(t, err)

	j, err := c.LockJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, j)

	t.Cleanup(func() {
		err := j.Done(ctx)
		assert.NoError(t, err)
	})

	assert.Equal(t, want, j.Queue)
}

func TestEnqueueWithEmptyType(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testEnqueueWithEmptyType(t, openFunc(t))
		})
	}
}

func testEnqueueWithEmptyType(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	err = c.Enqueue(ctx, &Job{Type: ""})
	require.Equal(t, ErrMissingType, err)
}

func TestEnqueueTx(t *testing.T) {
	for name, openFunc := range adapterTesting.AllAdaptersOpenTestPool {
		t.Run(name, func(t *testing.T) {
			testEnqueueTx(t, openFunc(t))
		})
	}
}

func testEnqueueTx(t *testing.T, connPool adapter.ConnPool) {
	ctx := context.Background()

	c, err := NewClient(connPool)
	require.NoError(t, err)

	tx, err := connPool.Begin(ctx)
	require.NoError(t, err)

	job := Job{Type: "MyJob"}
	err = c.EnqueueTx(ctx, &job, tx)
	require.NoError(t, err)

	j := findOneJob(t, tx)
	require.NotNil(t, j)

	err = tx.Rollback(ctx)
	require.NoError(t, err)

	j = findOneJob(t, connPool)
	require.Nil(t, j)
}
