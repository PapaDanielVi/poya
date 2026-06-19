// Package zap implements the poya logger.Logger interface on top of
// go.uber.org/zap. Pass a configured *zap.Logger and poya will log through it.
package zap

import (
	"go.uber.org/zap"

	"github.com/PapaDanielVi/poya/logger"
)

// zapLogger adapts a zap sugared logger to the poya Logger interface.
type zapLogger struct {
	l *zap.SugaredLogger
}

// New returns a poya Logger backed by the given zap logger. The caller owns the
// logger and is responsible for flushing it (zap.Logger.Sync) on shutdown.
func New(l *zap.Logger) logger.Logger {
	return &zapLogger{l: l.Sugar()}
}

func (z *zapLogger) Debug(msg string, keysAndValues ...any) {
	z.l.Debugw(msg, keysAndValues...)
}

func (z *zapLogger) Info(msg string, keysAndValues ...any) {
	z.l.Infow(msg, keysAndValues...)
}

func (z *zapLogger) Warn(msg string, keysAndValues ...any) {
	z.l.Warnw(msg, keysAndValues...)
}

func (z *zapLogger) Error(msg string, keysAndValues ...any) {
	z.l.Errorw(msg, keysAndValues...)
}
