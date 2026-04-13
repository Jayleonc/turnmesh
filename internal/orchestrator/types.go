package orchestrator

import (
	"context"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/executor"
	"github.com/Jayleonc/turnmesh/internal/feedback"
	"github.com/Jayleonc/turnmesh/internal/model"
)

// Preparer normalizes a turn before it reaches the model session.
type Preparer interface {
	PrepareTurn(context.Context, core.TurnInput) (core.TurnInput, error)
}

// Finalizer observes the completed turn report after the model/tool loop ends.
type Finalizer interface {
	FinalizeTurn(context.Context, TurnReport) error
}

// TurnReport captures the requested turn, the prepared turn, and all emitted events.
type TurnReport struct {
	Requested core.TurnInput
	Prepared  core.TurnInput
	Events    []core.TurnEvent
}

// Config wires the orchestrator to the abstract model, executor and feedback layers.
type Config struct {
	Preparer  Preparer
	Finalizer Finalizer
	Session   model.Session
	Tools     executor.Dispatcher
	ToolBatch executor.BatchRuntime
	Sink      feedback.Sink
}

type noopSink struct{}

func (noopSink) Emit(context.Context, feedback.Event) error { return nil }
