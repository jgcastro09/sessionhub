// Package events is the project-scoped publish/subscribe bus that
// internal/tasks and internal/registry use to notify the TUI, the Web Panel
// SSE stream, and (eventually) an agent bridge that something changed.
//
// It intentionally carries only project_id, kind, a monotonic per-project
// revision, and a small payload — never filesystem paths, credentials, or
// terminal output.
package events

import (
	"sync"
	"time"
)

// Event is the wire shape published to every subscriber of a project.
type Event struct {
	ProjectID string    `json:"project_id"`
	Kind      string    `json:"kind"`
	Revision  int64     `json:"revision"`
	Payload   any       `json:"payload,omitempty"`
	At        time.Time `json:"at"`
}

// Well-known event kinds. Services are free to define their own as long as
// they stay short, stable strings ("<domain>.<action>").
const (
	KindTaskCreated     = "task.created"
	KindTaskUpdated     = "task.updated"
	KindTaskStatus      = "task.status"
	KindTaskAudited     = "task.audited"
	KindRegistryScan    = "registry.scan"
	KindRegistryUpdated = "registry.updated"
)

type subscriber struct {
	id int
	ch chan Event
}

// Bus fans out events per project. Publish never blocks on a slow
// subscriber: a full subscriber channel drops the event rather than stalling
// the writer that owns the actual state change. Subscribers should treat a
// gap in Revision as "refetch," the same way the existing SSE heartbeat
// already asks the frontend to do.
type Bus struct {
	mu       sync.Mutex
	revision map[string]int64
	subs     map[string][]subscriber
	nextID   int
}

// NewBus constructs an empty Bus.
func NewBus() *Bus {
	return &Bus{
		revision: make(map[string]int64),
		subs:     make(map[string][]subscriber),
	}
}

// Subscribe opens a channel of events for one project. Call the returned
// cancel func to stop receiving and release the channel.
func (b *Bus) Subscribe(projectID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	ch := make(chan Event, 32)
	b.subs[projectID] = append(b.subs[projectID], subscriber{id: id, ch: ch})
	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		list := b.subs[projectID]
		for i, sub := range list {
			if sub.id == id {
				b.subs[projectID] = append(list[:i], list[i+1:]...)
				close(sub.ch)
				break
			}
		}
		if len(b.subs[projectID]) == 0 {
			delete(b.subs, projectID)
		}
	}
	return ch, cancel
}

// Publish increments the project's revision and fans the event out to every
// current subscriber. It returns the published event (with Revision and At
// filled in) so callers can embed it in an API response or Audit Report.
func (b *Bus) Publish(projectID, kind string, payload any) Event {
	b.mu.Lock()
	b.revision[projectID]++
	event := Event{
		ProjectID: projectID,
		Kind:      kind,
		Revision:  b.revision[projectID],
		Payload:   payload,
		At:        time.Now().UTC(),
	}
	subs := append([]subscriber(nil), b.subs[projectID]...)
	b.mu.Unlock()
	for _, sub := range subs {
		select {
		case sub.ch <- event:
		default:
		}
	}
	return event
}

// Revision reports the current revision counter for a project without
// publishing anything.
func (b *Bus) Revision(projectID string) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.revision[projectID]
}
