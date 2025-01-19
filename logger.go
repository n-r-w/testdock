package testdock

import "testing"

// Logger interface for custom logging.
type Logger interface {
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

// DefaultLogger is the default logger.
type DefaultLogger struct {
	t testing.TB
}

// NewDefaultLogger creates a new default logger.
func NewDefaultLogger(t testing.TB) Logger {
	return &DefaultLogger{t: t}
}

// Fatalf logs a fatal error.
func (d DefaultLogger) Fatalf(format string, args ...any) {
	d.t.Fatalf(format, args...)
}

// Logf logs a message.
func (d DefaultLogger) Logf(format string, args ...any) {
	d.t.Logf(format, args...)
}

// GooseLogger is a logger for goose.
type GooseLogger struct {
	l Logger
}

// NewGooseLogger creates a new goose logger.
func NewGooseLogger(l Logger) *GooseLogger {
	return &GooseLogger{l: l}
}

// Fatalf logs a fatal error.
func (l GooseLogger) Fatalf(format string, v ...any) {
	l.l.Fatalf(format, v...)
}

// Printf logs a message.
func (l GooseLogger) Printf(format string, v ...any) {
	l.l.Logf(format, v...)
}

// GolangMigrateLogger is a logger for golang-migrate.
type GolangMigrateLogger struct {
	l Logger
}

// NewGolangMigrateLogger creates a new golang-migrate logger.
func NewGolangMigrateLogger(l Logger) *GolangMigrateLogger {
	return &GolangMigrateLogger{l: l}
}

// Printf logs a message.
func (g *GolangMigrateLogger) Printf(format string, v ...interface{}) {
	g.l.Logf(format, v...)
}

// Verbose returns true.
func (g *GolangMigrateLogger) Verbose() bool {
	return true
}
