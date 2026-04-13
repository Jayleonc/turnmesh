package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
)

func TestBatchRuntimePlanGroupsByConcurrencySafety(t *testing.T) {
	registry := NewRegistryStore()
	mustRegisterTestTool(t, registry, "safe-a", true, nil)
	mustRegisterTestTool(t, registry, "safe-b", true, nil)
	mustRegisterTestTool(t, registry, "unsafe", false, nil)
	mustRegisterTestTool(t, registry, "safe-c", true, nil)

	runtime := NewBatchRuntime(registry)
	plan, err := runtime.Plan(context.Background(), []core.ToolInvocation{
		{ID: "1", Tool: "safe-a"},
		{ID: "2", Tool: "safe-b"},
		{ID: "3", Tool: "unsafe"},
		{ID: "4", Tool: "safe-c"},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if len(plan) != 3 {
		t.Fatalf("len(plan) = %d, want 3", len(plan))
	}
	if plan[0].Mode != BatchModeConcurrent || len(plan[0].Calls) != 2 {
		t.Fatalf("plan[0] = %#v, want concurrent batch of 2", plan[0])
	}
	if plan[1].Mode != BatchModeSerial || len(plan[1].Calls) != 1 || plan[1].Calls[0].Tool != "unsafe" {
		t.Fatalf("plan[1] = %#v, want serial unsafe batch", plan[1])
	}
	if plan[2].Mode != BatchModeConcurrent || len(plan[2].Calls) != 1 || plan[2].Calls[0].Tool != "safe-c" {
		t.Fatalf("plan[2] = %#v, want trailing concurrent batch", plan[2])
	}
}

func TestBatchRuntimeRunPreservesInputOrderAcrossConcurrentCompletion(t *testing.T) {
	registry := NewRegistryStore()
	mustRegisterTestTool(t, registry, "slow", true, func(context.Context, ToolRequest) (ToolOutcome, error) {
		time.Sleep(40 * time.Millisecond)
		return ToolOutcome{Output: "slow", Status: core.ToolStatusSucceeded}, nil
	})
	mustRegisterTestTool(t, registry, "fast", true, func(context.Context, ToolRequest) (ToolOutcome, error) {
		return ToolOutcome{Output: "fast", Status: core.ToolStatusSucceeded}, nil
	})

	report, err := NewBatchRuntime(registry).Run(context.Background(), []core.ToolInvocation{
		{ID: "tool-1", Tool: "slow"},
		{ID: "tool-2", Tool: "fast"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(report.Results) != 2 {
		t.Fatalf("len(report.Results) = %d, want 2", len(report.Results))
	}
	if report.Results[0].Tool != "slow" || report.Results[1].Tool != "fast" {
		t.Fatalf("results = %#v, want slow/fast in call order", report.Results)
	}
}

func TestBatchRuntimeRunCascadesFailureAndDiscardsRemainingBatches(t *testing.T) {
	registry := NewRegistryStore()
	mustRegisterTestTool(t, registry, "safe", true, func(context.Context, ToolRequest) (ToolOutcome, error) {
		return ToolOutcome{Output: "ok", Status: core.ToolStatusSucceeded}, nil
	})
	mustRegisterTestTool(t, registry, "unsafe-fail", false, func(context.Context, ToolRequest) (ToolOutcome, error) {
		return ToolOutcome{}, errors.New("boom")
	})
	mustRegisterTestTool(t, registry, "safe-late", true, func(context.Context, ToolRequest) (ToolOutcome, error) {
		return ToolOutcome{Output: "late", Status: core.ToolStatusSucceeded}, nil
	})

	report, err := NewBatchRuntime(registry).Run(context.Background(), []core.ToolInvocation{
		{ID: "tool-1", Tool: "safe"},
		{ID: "tool-2", Tool: "unsafe-fail"},
		{ID: "tool-3", Tool: "safe-late"},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want failure")
	}
	if !report.Failed {
		t.Fatal("report.Failed = false, want true")
	}
	if len(report.Results) != 3 {
		t.Fatalf("len(report.Results) = %d, want 3", len(report.Results))
	}
	if report.Results[0].Status != core.ToolStatusSucceeded {
		t.Fatalf("first result status = %q, want succeeded", report.Results[0].Status)
	}
	if report.Results[1].Status != core.ToolStatusFailed {
		t.Fatalf("second result status = %q, want failed", report.Results[1].Status)
	}
	if report.Results[2].Status != core.ToolStatusCancelled {
		t.Fatalf("third result status = %q, want cancelled", report.Results[2].Status)
	}
	if report.Results[2].Metadata["discarded"] != "true" {
		t.Fatalf("discard metadata = %#v, want discarded result", report.Results[2].Metadata)
	}
}

func TestBatchRuntimeStreamEmitsLifecycleEvents(t *testing.T) {
	registry := NewRegistryStore()
	mustRegisterTestTool(t, registry, "safe", true, func(context.Context, ToolRequest) (ToolOutcome, error) {
		return ToolOutcome{Output: "ok", Status: core.ToolStatusSucceeded}, nil
	})

	stream, err := NewBatchRuntime(registry).Stream(context.Background(), []core.ToolInvocation{
		{ID: "tool-1", Tool: "safe"},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var events []BatchEventKind
	for event := range stream.Events {
		events = append(events, event.Kind)
	}
	report := <-stream.Done

	if len(events) < 3 {
		t.Fatalf("len(events) = %d, want >= 3", len(events))
	}
	if events[0] != BatchEventBatchStarted {
		t.Fatalf("events[0] = %q, want batch_started", events[0])
	}
	if events[len(events)-1] != BatchEventCompleted {
		t.Fatalf("last event = %q, want completed", events[len(events)-1])
	}
	if len(report.Results) != 1 || report.Results[0].Status != core.ToolStatusSucceeded {
		t.Fatalf("report = %#v, want one successful result", report)
	}
}

func mustRegisterTestTool(t *testing.T, registry *RegistryStore, name string, concurrencySafe bool, handler ToolHandler) {
	t.Helper()

	if handler == nil {
		handler = func(context.Context, ToolRequest) (ToolOutcome, error) {
			return ToolOutcome{Status: core.ToolStatusSucceeded}, nil
		}
	}

	if err := registry.Register(NewHandlerTool(ToolSpec{
		Name:            name,
		ConcurrencySafe: concurrencySafe,
	}, handler)); err != nil {
		t.Fatalf("Register(%s) error = %v", name, err)
	}
}
