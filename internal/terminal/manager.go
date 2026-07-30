package terminal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nodestage/sessionhub/internal/domain"
)

type Manager struct {
	ctx        context.Context
	sink       HistorySink
	scrollback int
	events     chan Event
	mu         sync.RWMutex
	sessions   map[string]*Session
}

func NewManager(ctx context.Context, sink HistorySink, scrollback int) *Manager {
	if scrollback < 0 {
		scrollback = 0
	}
	return &Manager{
		ctx: ctx, sink: sink, scrollback: scrollback,
		events: make(chan Event, 4096), sessions: make(map[string]*Session),
	}
}

func (m *Manager) Events() <-chan Event { return m.events }

func (m *Manager) Start(instanceID string, cfg domain.ExecutorConfig, width, height int) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[instanceID]; exists {
		return nil, fmt.Errorf("terminal instance %q is already active", instanceID)
	}
	session, err := Start(m.ctx, instanceID, cfg, width, height, m.scrollback, m.sink, m.events)
	if err != nil {
		return nil, err
	}
	m.sessions[instanceID] = session
	return session, nil
}

func (m *Manager) StartEphemeral(
	instanceID string,
	cfg domain.ExecutorConfig,
	width, height int,
) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[instanceID]; exists {
		return nil, fmt.Errorf("terminal instance %q is already active", instanceID)
	}
	session, err := Start(m.ctx, instanceID, cfg, width, height, m.scrollback, nil, m.events)
	if err != nil {
		return nil, err
	}
	m.sessions[instanceID] = session
	return session, nil
}

func (m *Manager) Get(instanceID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[instanceID]
	return session, ok
}

func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		out = append(out, session)
	}
	return out
}

func (m *Manager) Stop(instanceID string, grace time.Duration) error {
	m.mu.Lock()
	session, ok := m.sessions[instanceID]
	if ok {
		delete(m.sessions, instanceID)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("terminal instance %q is not active", instanceID)
	}
	return session.Close(grace)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	var first error
	for _, session := range sessions {
		if err := session.Close(2 * time.Second); err != nil && first == nil {
			first = err
		}
	}
	return first
}
