package agent

// TaskLifecycleStateMachine validates task lifecycle transitions.
type TaskLifecycleStateMachine struct{}

// CanTransition reports whether a transition is allowed.
func (TaskLifecycleStateMachine) CanTransition(from, to TaskStatus) bool {
	switch from {
	case TaskStatusUnknown:
		return to == TaskStatusPending
	case TaskStatusPending:
		switch to {
		case TaskStatusRunning, TaskStatusCompleted, TaskStatusFailed, TaskStatusStopped:
			return true
		default:
			return false
		}
	case TaskStatusRunning:
		switch to {
		case TaskStatusCompleted, TaskStatusFailed, TaskStatusStopped:
			return true
		default:
			return false
		}
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusStopped:
		return from == to
	default:
		return false
	}
}

// Transition validates a lifecycle transition and returns an error if it is not allowed.
func (m TaskLifecycleStateMachine) Transition(from, to TaskStatus) error {
	if m.CanTransition(from, to) {
		return nil
	}
	return ErrInvalidTaskTransition
}
