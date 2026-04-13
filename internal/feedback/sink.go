package feedback

import "context"

// Sink consumes structured feedback events.
type Sink interface {
	Emit(ctx context.Context, event Event) error
}
