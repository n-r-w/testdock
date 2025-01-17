package pglive

import "testing"

// Logger interface for custom logging.
type Logger interface {
	Fatal(args ...any)
	Log(args ...any)
	Logf(format string, args ...any)
}

type defaultLogger struct {
	t testing.TB
}

func (d defaultLogger) Fatal(args ...any) {
	d.t.Fatal(args...)
}

func (d defaultLogger) Log(args ...any) {
	d.t.Log(args...)
}

func (d defaultLogger) Logf(format string, args ...any) {
	d.t.Logf(format, args...)
}
