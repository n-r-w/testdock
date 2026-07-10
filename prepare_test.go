package testdock

import (
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatabaseConcurrencyStateLimitsOperations verifies that preparation and cleanup for one DSN
// share a single capacity limit.
func TestDatabaseConcurrencyStateLimitsOperations(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// ARRANGE: Start one more request than the configured database operation limit.
		const requestCount = maxParallelDatabaseOperations + 1

		state := newDatabaseConcurrencyState()
		started := make(chan struct{}, requestCount)
		release := make(chan struct{})
		errors := make(chan error, requestCount)
		var workers sync.WaitGroup

		// ACT: Keep every admitted database operation active until the shared limit is observed.
		for range requestCount {
			workers.Go(func() {
				errors <- state.runDatabaseOperation(func() error {
					started <- struct{}{}
					<-release
					return nil
				})
			})
		}

		for range maxParallelDatabaseOperations {
			<-started
		}
		synctest.Wait()

		// ASSERT: The extra request must remain blocked until an active preparation finishes.
		startedBeyondLimit := false
		select {
		case <-started:
			startedBeyondLimit = true
		default:
		}

		close(release)
		workers.Wait()
		close(errors)

		assert.False(t, startedBeyondLimit)
		for err := range errors {
			require.NoError(t, err)
		}
	})
}

func checkInformer(t *testing.T, defaultDSN string, informer Informer) {
	t.Helper()

	defaultURL, err := parseURL(defaultDSN)
	require.NoError(t, err)

	url, err := parseURL(informer.DSN())
	require.NoError(t, err)

	require.NotEqual(t, defaultURL.Database, url.Database)
	require.NotEqual(t, defaultURL.Database, informer.DatabaseName())
}
