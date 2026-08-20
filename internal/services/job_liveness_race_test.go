package services

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// TestEffectiveJobStatus_DoesNotBlockAcquire proves the liveness probe stays out
// of the way. The probe must take the lock to find out if it is free, thus a
// `job list` that runs at the same time as `pipeline start` could make the start
// refuse to run. The help text tells users to run `watch -n 5 aether job list`,
// so the two commands do run together.
func TestEffectiveJobStatus_DoesNotBlockAcquire(t *testing.T) {
	jobsDir := t.TempDir()
	job := models.PipelineJob{JobID: "job-1", Status: models.JobStatusInProgress}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = EffectiveJobStatus(jobsDir, job)
			}
		}
	}()

	failures := 0
	const attempts = 500
	for range attempts {
		lock, err := AcquireJobLock(jobsDir, job.JobID, testLockLogger())
		if err != nil {
			failures++
			continue
		}
		_ = lock.Release()
	}
	close(stop)
	wg.Wait()

	assert.Zero(t, failures, "the probe made %d of %d acquisitions fail", failures, attempts)
}

// TestAcquireJobLock_StillRefusesALiveHolder proves the retry that absorbs the
// probe does not weaken the guard against two runs of the same job.
func TestAcquireJobLock_StillRefusesALiveHolder(t *testing.T) {
	jobsDir := t.TempDir()

	held, err := AcquireJobLock(jobsDir, "job-1", testLockLogger())
	assert.NoError(t, err)
	defer func() { _ = held.Release() }()

	second, err := AcquireJobLock(jobsDir, "job-1", testLockLogger())
	assert.Error(t, err)
	assert.Nil(t, second)
}
