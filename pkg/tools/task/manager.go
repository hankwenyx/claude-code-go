package task

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// TaskStatus represents the lifecycle state of an async task.
type TaskStatus string

const (
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
)

// AsyncTask holds the state of a background sub-agent execution.
type AsyncTask struct {
	ID          string
	Description string
	Status      TaskStatus
	Result      string
	Err         error
	StartedAt   time.Time
	FinishedAt  time.Time
}

// Notification is sent on the Manager's channel when an async task finishes.
type Notification struct {
	Task *AsyncTask
}

// Manager tracks async sub-agent tasks and delivers completion notifications.
type Manager struct {
	mu       sync.RWMutex
	tasks    map[string]*AsyncTask
	notifyCh chan Notification
}

// NewManager creates a Manager with the given notification channel buffer size.
func NewManager(bufSize int) *Manager {
	return &Manager{
		tasks:    make(map[string]*AsyncTask),
		notifyCh: make(chan Notification, bufSize),
	}
}

// NotifyCh returns the channel on which task-completion notifications are sent.
// Consumers should drain this channel regularly.
func (m *Manager) NotifyCh() <-chan Notification {
	return m.notifyCh
}

// Dispatch starts a sub-agent in a background goroutine and returns the task ID.
func (m *Manager) Dispatch(ctx context.Context, description, prompt string, runner SubAgentRunner) string {
	id := fmt.Sprintf("%016x", rand.Int63())
	t := &AsyncTask{
		ID:          id,
		Description: description,
		Status:      StatusRunning,
		StartedAt:   time.Now(),
	}
	m.mu.Lock()
	m.tasks[id] = t
	m.mu.Unlock()

	go func() {
		result, err := runner(ctx, prompt)
		m.mu.Lock()
		t.FinishedAt = time.Now()
		t.Result = result
		t.Err = err
		if err != nil {
			t.Status = StatusFailed
		} else {
			t.Status = StatusCompleted
		}
		m.mu.Unlock()

		// Non-blocking send — drop notification if nobody is listening.
		select {
		case m.notifyCh <- Notification{Task: t}:
		default:
		}
	}()

	return id
}

// Get returns a snapshot of the task with the given ID, or nil if not found.
func (m *Manager) Get(id string) *AsyncTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t := m.tasks[id]
	if t == nil {
		return nil
	}
	// return a copy
	cp := *t
	return &cp
}

// DrainNotifications returns and removes all pending notifications without blocking.
func (m *Manager) DrainNotifications() []Notification {
	var out []Notification
	for {
		select {
		case n := <-m.notifyCh:
			out = append(out, n)
		default:
			return out
		}
	}
}
