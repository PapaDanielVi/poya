// Package zerolog implements the poya logger.Logger interface on top of
// github.com/rs/zerolog. Pass a configured zerolog.Logger and poya will log
// through it.
package zerolog

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/PapaDanielVi/poya/logger"
)

// zerologLogger adapts a zerolog logger to the poya Logger interface.
type zerologLogger struct {
	l zerolog.Logger
}

// New returns a poya Logger backed by the given zerolog logger. zerolog.Logger
// is a value type, so pass it directly (e.g. zerolog.New(os.Stderr)).
func New(l zerolog.Logger) logger.Logger {
	return &zerologLogger{l: l}
}

func (z *zerologLogger) Debug(msg string, keysAndValues ...any) {
	emit(z.l.Debug(), msg, keysAndValues)
}

func (z *zerologLogger) Info(msg string, keysAndValues ...any) {
	emit(z.l.Info(), msg, keysAndValues)
}

func (z *zerologLogger) Warn(msg string, keysAndValues ...any) {
	emit(z.l.Warn(), msg, keysAndValues)
}

func (z *zerologLogger) Error(msg string, keysAndValues ...any) {
	emit(z.l.Error(), msg, keysAndValues)
}

// emit attaches the flat key/value pairs to the event and writes it. Non-string
// keys are rendered with %v, and a trailing key without a value is given an empty
// value so an odd-length slice never panics.
func emit(e *zerolog.Event, msg string, keysAndValues []any) {
	for i := 0; i < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		if i+1 < len(keysAndValues) {
			e = e.Interface(key, keysAndValues[i+1])
		} else {
			e = e.Interface(key, "")
		}
	}
	e.Msg(msg)
}
