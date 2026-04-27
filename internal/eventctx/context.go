package eventctx

import (
	"context"

	"github.com/Jayleonc/turnmesh/internal/core"
)

type emitterKey struct{}

type Emitter func(core.TurnEvent) bool

func WithEmitter(ctx context.Context, emitter Emitter) context.Context {
	if ctx == nil || emitter == nil {
		return ctx
	}
	return context.WithValue(ctx, emitterKey{}, emitter)
}

func EmitterFromContext(ctx context.Context) (Emitter, bool) {
	if ctx == nil {
		return nil, false
	}
	emitter, ok := ctx.Value(emitterKey{}).(Emitter)
	if !ok || emitter == nil {
		return nil, false
	}
	return emitter, true
}

func Emit(ctx context.Context, event core.TurnEvent) bool {
	emitter, ok := EmitterFromContext(ctx)
	if !ok {
		return false
	}
	return emitter(event)
}
