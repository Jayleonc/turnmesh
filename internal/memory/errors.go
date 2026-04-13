package memory

import "errors"

var (
	ErrNotFound    = errors.New("memory entry not found")
	ErrInvalidID   = errors.New("memory entry id is required")
	ErrInvalidPath = errors.New("memory store path is required")
	ErrCorruptFile = errors.New("memory store file is corrupt")
)
