// Package evidentzap carries observations through zap: production code logs a
// regular structured line, tests capture the same line into an evident.Ledger.
package evidentzap

import (
	"github.com/inhuman/evident"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const Message = "evident"

// Observe logs the facts seen at a decision point. In production it is an
// ordinary log line ({"msg":"evident","spec":...,"facts":{...}}) that ad-hoc
// log scripts can grep; under test Core turns it into an Observation.
func Observe(l *zap.Logger, spec string, facts ...zap.Field) {
	l.Info(Message, zap.String("spec", spec), zap.Dict("facts", facts...))
}

// Capture returns l with a core that also records Observe calls into ledger.
func Capture(l *zap.Logger, ledger *evident.Ledger) *zap.Logger {
	return l.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return zapcore.NewTee(c, Core(ledger))
	}))
}

// Core records Observe entries into ledger and drops everything else.
func Core(ledger *evident.Ledger) zapcore.Core {
	return &core{ledger: ledger}
}

type core struct {
	ledger *evident.Ledger
}

func (c *core) Enabled(zapcore.Level) bool { return true }

func (c *core) With([]zapcore.Field) zapcore.Core { return c }

func (c *core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if ent.Message != Message {
		return ce
	}
	return ce.AddCore(ent, c)
}

func (c *core) Write(_ zapcore.Entry, fields []zapcore.Field) error {
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}
	spec, _ := enc.Fields["spec"].(string)
	facts, _ := enc.Fields["facts"].(map[string]any)
	c.ledger.Record(evident.Observation{Spec: spec, Source: evident.Impl, Facts: facts})
	return nil
}

func (c *core) Sync() error { return nil }
