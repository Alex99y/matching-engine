package observability

import "testing"

func TestNewServiceRegistry(t *testing.T) {
	reg := NewServiceRegistry()
	if reg == nil {
		t.Fatal("expected a non-nil registry")
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(mfs) == 0 {
		t.Error("expected the Go/process collectors to have registered at least one metric family")
	}
}

func TestNewSubsystemMetrics(t *testing.T) {
	reg := NewServiceRegistry()
	metrics := NewSubsystemMetrics(reg, "me", "api")
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if metrics.namespace != "me" {
		t.Errorf("namespace = %q, want %q", metrics.namespace, "me")
	}
	if metrics.subsystem != "api" {
		t.Errorf("subsystem = %q, want %q", metrics.subsystem, "api")
	}
	if metrics.registerer != reg {
		t.Error("expected the metrics to be bound to the given registry")
	}
}

func TestNewSubsystemMetricsIndependentPerSubsystem(t *testing.T) {
	reg := NewServiceRegistry()
	api := NewSubsystemMetrics(reg, "me", "api")
	db := NewSubsystemMetrics(reg, "me", "db")

	if api.subsystem == db.subsystem {
		t.Error("expected distinct subsystems to stay independent")
	}
	if api.registerer != db.registerer {
		t.Error("expected both subsystems to share the same underlying registry")
	}
}
