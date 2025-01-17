package pglive

import "testing"

// Logger interface for custom logging.
type Logger interface {
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

type defaultLogger struct {
	t testing.TB
}

func (d defaultLogger) Fatalf(format string, args ...any) {
	d.t.Fatalf(format, args...)
}

func (d defaultLogger) Logf(format string, args ...any) {
	d.t.Logf(format, args...)
}

type gooseLogger struct {
	l Logger
}

func (l gooseLogger) Fatalf(format string, v ...any) {
	l.l.Fatalf(format, v...)
}

func (l gooseLogger) Printf(format string, v ...any) {
	l.l.Logf(format, v...)
}
