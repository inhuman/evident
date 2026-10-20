package evidentzap

import (
	"testing"
	"time"

	"github.com/inhuman/evident"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestObserveIsCapturedAndStillLogged(t *testing.T) {
	logCore, logs := observer.New(zap.InfoLevel)
	var ledger evident.Ledger
	Attach(&ledger)
	t.Cleanup(func() { Attach(nil) })
	l := zap.New(logCore)

	l.Info("unrelated")
	Observe(l.With(zap.String("scope", "dns")), "dns.forward",
		zap.String("route", "corp"), zap.Bool("answered", true), zap.Int("rcode", 0), zap.Strings("upstreams", []string{"10.0.0.1"}))

	obs := ledger.Observations()
	if len(obs) != 1 {
		t.Fatalf("captured %d observations, want 1: %+v", len(obs), obs)
	}
	o := obs[0]
	if o.Spec != "dns.forward" || o.Source != evident.Impl {
		t.Fatalf("observation = %+v", o)
	}
	if o.Facts.String("route") != "corp" || !o.Facts.Bool("answered") || o.Facts.Int("rcode") != 0 || o.Facts.Strings("upstreams")[0] != "10.0.0.1" {
		t.Fatalf("facts = %v", o.Facts)
	}
	if logs.Len() != 2 {
		t.Fatalf("production log must keep both lines, got %d", logs.Len())
	}
	if got := logs.All()[1].ContextMap()["spec"]; got != "dns.forward" {
		t.Fatalf("log line spec = %v", got)
	}
}

// A logger with a raised level or a sampler must not be able to drop evidence.
func TestLoggerFiltersCannotDropEvidence(t *testing.T) {
	var ledger evident.Ledger
	Attach(&ledger)
	t.Cleanup(func() { Attach(nil) })

	quiet := zap.NewNop().WithOptions(zap.IncreaseLevel(zapcore.ErrorLevel))
	sampled := zap.NewNop().WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return zapcore.NewSamplerWithOptions(c, time.Hour, 1, 0)
	}))
	for range 3 {
		Observe(quiet, "x", zap.Bool("v", true))
		Observe(sampled, "x", zap.Bool("v", true))
	}
	if n := len(ledger.Observations()); n != 6 {
		t.Fatalf("recorded %d observations, want 6", n)
	}
}
