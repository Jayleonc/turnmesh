package agent

import "context"

// Registry resolves agent definitions from storage or configuration.
type Registry interface {
	List(ctx context.Context) ([]Definition, error)
	Get(ctx context.Context, id string) (Definition, error)
}
