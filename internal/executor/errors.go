package executor

import "errors"

var (
	ErrDuplicateTool = errors.New("executor: tool already registered")
	ErrToolNotFound  = errors.New("executor: tool not found")
)
