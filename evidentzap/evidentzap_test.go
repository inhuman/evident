package evidentzap

import (
	"testing"

	"github.com/inhuman/evident"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestObserveIsCapturedAndStillLogged(t *testing.T) {
	logCore, logs := observer.New(zap.InfoLevel)
	var ledger evident.Ledger
	l := Capture(zap.New(logCore), &ledger)

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
