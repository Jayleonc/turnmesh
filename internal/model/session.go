package model

import (
	"context"

	"github.com/Jayleonc/turnmesh/internal/core"
)

type Session interface {
	ID() string
	Provider() string
	Model() string
	Capabilities() Capabilities
	StreamTurn(ctx context.Context, input core.TurnInput) (<-chan core.TurnEvent, error)
	Close() error
}
