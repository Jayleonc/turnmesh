package agent

import (
	"sort"
	"strings"
	"sync"
)

type taskRegistry struct {
	mu    sync.RWMutex
	tasks map[string]*taskHandle
}

func newTaskRegistry() *taskRegistry {
	return &taskRegistry{
		tasks: make(map[string]*taskHandle),
	}
}

func (r *taskRegistry) register(task *taskHandle) error {
	if r == nil {
		return ErrNilRuntime
	}
	if task == nil {
		return ErrTaskNotFound
	}

	id := strings.TrimSpace(task.ID())
	if id == "" {
		return ErrTaskNotFound
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[id]; exists {
		return ErrTaskAlreadyExists
	}
	r.tasks[id] = task
	return nil
}

func (r *taskRegistry) lookup(taskID string) (*taskHandle, bool) {
	if r == nil {
		return nil, false
	}

	key := strings.TrimSpace(taskID)
	if key == "" {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	task, ok := r.tasks[key]
	return task, ok
}

func (r *taskRegistry) snapshot(taskID string) (TaskSnapshot, bool) {
	task, ok := r.lookup(taskID)
	if !ok {
		return TaskSnapshot{}, false
	}
	return task.Snapshot(), true
}

func (r *taskRegistry) list() []TaskSnapshot {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.tasks))
	for id := range r.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]TaskSnapshot, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.tasks[id].Snapshot())
	}
	return out
}
