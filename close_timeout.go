package testdock

import (
	"context"
	"errors"
	"fmt"
	"runtime/pprof"
	"strings"
	"time"
)

// goroutineProfileDebug selects panic-style stack output for the goroutine profile.
const goroutineProfileDebug = 2

// pgxPoolCloseStats contains pgxpool statistics that explain a close timeout.
type pgxPoolCloseStats struct {
	AcquiredConns     int32
	IdleConns         int32
	TotalConns        int32
	ConstructingConns int32
	MaxConns          int32
}

// closeTimeoutDiagnostics contains fields printed when a returned resource close times out.
type closeTimeoutDiagnostics struct {
	TestName      string
	Resource      string
	RedactedDSN   string
	DatabaseName  string
	Timeout       time.Duration
	PgxStats      *pgxPoolCloseStats
	GoroutineDump string
}

// closeResourceWithTimeout closes a returned resource with a bounded wait.
func closeResourceWithTimeout(timeout time.Duration, closeResource func() error, diagnostics func() string) error {
	done := make(chan struct{})
	go func() {
		_ = closeResource()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		message := fmt.Sprintf("close timed out after %s", timeout)
		if diagnostics != nil {
			if details := diagnostics(); details != "" {
				message += "\n" + details
			}
		}
		return errors.New(message)
	}
}

// disconnectWithTimeout disconnects a returned client with a timeout-aware context.
func disconnectWithTimeout(timeout time.Duration, disconnect func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := disconnect(ctx)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("close timed out after %s", timeout)
	}

	return nil
}

// formatCloseTimeoutDiagnostics formats close timeout diagnostics.
func formatCloseTimeoutDiagnostics(d closeTimeoutDiagnostics) string {
	lines := []string{
		"test: " + d.TestName,
		"resource: " + d.Resource,
		"dsn: " + d.RedactedDSN,
		"database: " + d.DatabaseName,
		"timeout: " + d.Timeout.String(),
	}

	if d.PgxStats != nil {
		lines = append(lines,
			fmt.Sprintf("acquired_conns: %d", d.PgxStats.AcquiredConns),
			fmt.Sprintf("idle_conns: %d", d.PgxStats.IdleConns),
			fmt.Sprintf("total_conns: %d", d.PgxStats.TotalConns),
			fmt.Sprintf("constructing_conns: %d", d.PgxStats.ConstructingConns),
			fmt.Sprintf("max_conns: %d", d.PgxStats.MaxConns),
		)
	}

	lines = append(lines, "goroutine dump:", d.GoroutineDump)

	return strings.Join(lines, "\n")
}

// closeTimeoutDetails builds diagnostics for a returned resource close timeout.
func (d *testDB) closeTimeoutDetails(resource string, stats *pgxPoolCloseStats) string {
	redactedDSN := d.redactedTestDSN()
	goroutineDump := redactKnownSecrets(captureGoroutineDump(), d.rawTestDSN(), d.urlPassword())

	return formatCloseTimeoutDiagnostics(closeTimeoutDiagnostics{
		TestName:      d.t.Name(),
		Resource:      resource,
		RedactedDSN:   redactedDSN,
		DatabaseName:  d.databaseName,
		Timeout:       d.closeTimeout,
		PgxStats:      stats,
		GoroutineDump: goroutineDump,
	})
}

// redactedTestDSN returns the temporary database DSN without password.
func (d *testDB) redactedTestDSN() string {
	if d.url == nil {
		return d.dsnNoPass
	}

	return d.url.replaceDatabase(d.databaseName).string(true)
}

// rawTestDSN returns the temporary database DSN with password for diagnostic redaction only.
func (d *testDB) rawTestDSN() string {
	if d.url == nil {
		return d.dsn
	}

	return d.url.replaceDatabase(d.databaseName).string(false)
}

// urlPassword returns the configured password for diagnostic redaction only.
func (d *testDB) urlPassword() string {
	if d.url == nil {
		return ""
	}

	return d.url.Password
}

// captureGoroutineDump captures the runtime goroutine profile in panic-style text format.
func captureGoroutineDump() string {
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return "goroutine profile is unavailable"
	}

	var b strings.Builder
	if err := profile.WriteTo(&b, goroutineProfileDebug); err != nil {
		return fmt.Sprintf("goroutine profile write failed: %v", err)
	}

	return b.String()
}

// redactKnownSecrets removes known sensitive values from diagnostic text.
func redactKnownSecrets(text string, secrets ...string) string {
	redacted := text
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, secret, "*****")
	}

	return redacted
}
