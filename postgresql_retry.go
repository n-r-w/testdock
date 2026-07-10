package testdock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/n-r-w/ctxlog"
)

// postgresTooManyClientsSQLState identifies PostgreSQL connection-capacity rejection.
const postgresTooManyClientsSQLState = "53300"

// sqlStateError exposes a database error code without coupling retry logic to a specific driver.
type sqlStateError interface {
	SQLState() string
}

// retryPostgresOperation retries only PostgreSQL capacity errors within the configured duration.
func retryPostgresOperation(
	ctx context.Context,
	logger ctxlog.ILogger,
	retryTimeout, totalRetryDuration time.Duration,
	operationName string,
	operation func() error,
) error {
	var attempt int
	wrappedOperation := func() (struct{}, error) {
		err := operation()
		if err == nil {
			return struct{}{}, nil
		}
		if !hasSQLState(err, postgresTooManyClientsSQLState) {
			return struct{}{}, backoff.Permanent(err)
		}

		attempt++
		logger.Info(ctx, "retrying postgres operation", "operation", operationName, "attempt", attempt, "error", err)
		return struct{}{}, err
	}

	_, err := backoff.Retry(
		ctx,
		wrappedOperation,
		backoff.WithBackOff(backoff.NewConstantBackOff(retryTimeout)),
		backoff.WithMaxElapsedTime(totalRetryDuration),
	)
	if err == nil || !hasSQLState(err, postgresTooManyClientsSQLState) {
		return err
	}

	return fmt.Errorf("%s retry failed after %d attempts: %w", operationName, attempt, err)
}

// hasSQLState checks wrapped driver errors through their common SQLSTATE contract.
func hasSQLState(err error, state string) bool {
	var stateErr sqlStateError
	return errors.As(err, &stateErr) && stateErr.SQLState() == state
}
