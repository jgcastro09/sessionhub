package domain

import "fmt"

type State string

const (
	StatePending     State = "pending"
	StateWaiting     State = "waiting"
	StateRunning     State = "running"
	StatePaused      State = "paused"
	StateInterrupted State = "interrupted"
	StateSucceeded   State = "succeeded"
	StateFailed      State = "failed"
	StateCanceled    State = "canceled"
	StateFinished    State = "finished"
	StateError       State = "error"
)

var transitions = map[State]map[State]bool{
	StatePending: {
		StateWaiting: true, StateRunning: true, StatePaused: true,
		StateCanceled: true, StateFailed: true,
	},
	StateWaiting: {
		StatePending: true, StateRunning: true, StatePaused: true, StateCanceled: true,
		StateFailed: true, StateInterrupted: true,
	},
	StateRunning: {
		StatePending: true, StateWaiting: true, StatePaused: true, StateInterrupted: true,
		StateSucceeded: true, StateFailed: true, StateCanceled: true,
		StateFinished: true, StateError: true,
	},
	StatePaused: {
		StateWaiting: true, StateRunning: true, StateCanceled: true,
		StateInterrupted: true, StateFailed: true,
	},
	StateInterrupted: {
		StatePending: true, StateWaiting: true, StateRunning: true,
		StateCanceled: true, StateFailed: true,
	},
	StateSucceeded: {},
	StateFailed:    {StatePending: true},
	StateCanceled:  {},
	StateFinished:  {},
	StateError:     {StatePending: true, StateCanceled: true},
}

func (s State) Terminal() bool {
	switch s {
	case StateSucceeded, StateCanceled, StateFinished:
		return true
	default:
		return false
	}
}

func ValidateTransition(from, to State) error {
	next, ok := transitions[from]
	if !ok {
		return fmt.Errorf("unknown current state %q", from)
	}
	if !next[to] {
		return fmt.Errorf("invalid state transition %q -> %q", from, to)
	}
	return nil
}
