// Package evidentzap carries implementation observations through zap: production
// code logs a regular structured line, tests capture the same observation into
// an evident.Ledger.
package evidentzap

import (
	"sync/atomic"

	"github.com/inhuman/evident"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const Message = "evident"

var sink atomic.Pointer[evident.Ledger]

// Observe logs the facts seen at a decision point. In production it is an
// ordinary log line ({"msg":"evident","spec":...,"facts":{...}}) that ad-hoc
// log scripts can grep. When a ledger is attached (tests), the observation is
// recorded directly — log levels and samplers of l cannot drop evidence.
func Observe(l *zap.Logger, spec string, facts ...zap.Field) {
	if ledger := sink.Load(); ledger != nil {
		ledger.Record(evident.Observation{Spec: spec, Source: evident.Impl, Facts: decode(facts)})
	}
	l.Info(Message, zap.String("spec", spec), zap.Dict("facts", facts...))
}

// Attach routes every Observe of the process to ledger; detach with Attach(nil).
func Attach(ledger *evident.Ledger) {
	sink.Store(ledger)
}

// Capture attaches ledger and returns l unchanged; kept for call sites that
// build their logger in one expression.
func Capture(l *zap.Logger, ledger *evident.Ledger) *zap.Logger {
	Attach(ledger)
	return l
}

func decode(fields []zap.Field) evident.Facts {
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}
	return enc.Fields
}
