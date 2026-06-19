// Package logrus implements the poya logger.Logger interface on top of
// github.com/sirupsen/logrus. Pass a configured *logrus.Logger and poya will log
// through it.
package logrus

import (
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/PapaDanielVi/poya/logger"
)

// logrusLogger adapts a logrus logger to the poya Logger interface.
type logrusLogger struct {
	l *logrus.Logger
}

// New returns a poya Logger backed by the given logrus logger.
func New(l *logrus.Logger) logger.Logger {
	return &logrusLogger{l: l}
}

func (g *logrusLogger) Debug(msg string, keysAndValues ...any) {
	g.l.WithFields(toFields(keysAndValues)).Debug(msg)
}

func (g *logrusLogger) Info(msg string, keysAndValues ...any) {
	g.l.WithFields(toFields(keysAndValues)).Info(msg)
}

func (g *logrusLogger) Warn(msg string, keysAndValues ...any) {
	g.l.WithFields(toFields(keysAndValues)).Warn(msg)
}

func (g *logrusLogger) Error(msg string, keysAndValues ...any) {
	g.l.WithFields(toFields(keysAndValues)).Error(msg)
}

// toFields turns a flat key/value slice into logrus.Fields. Non-string keys are
// rendered with %v, and a trailing key without a value is given an empty value so
// an odd-length slice never panics.
func toFields(keysAndValues []any) logrus.Fields {
	fields := make(logrus.Fields, len(keysAndValues)/2+1)
	for i := 0; i < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		if i+1 < len(keysAndValues) {
			fields[key] = keysAndValues[i+1]
		} else {
			fields[key] = ""
		}
	}
	return fields
}
