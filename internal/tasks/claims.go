package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jgcastro09/sessionhub/internal/atomicfile"
)

// Claim records which Executor/terminal is currently working a task. Per
// plan 4.3, this is runtime state, not Git-tracked project state: it lives
// under the local runtime root (~/.sessionhub/projects/<project_id>/), never
// under .shproject, so multiple Executors can work a project without one
// overwriting another's claim and without polluting the repo. The Task
// Manager only shows claims and flags conflicts; it does not enforce a
// network lock (plan 4.3).
type Claim struct {
	TaskID     string    `json:"task_id"`
	ExecutorID string    `json:"executor_id"`
	TerminalID string    `json:"terminal_id"`
	ClaimedAt  time.Time `json:"claimed_at"`
}

func (s *Service) claimsPath(projectID string) string {
	return filepath.Join(s.runtimeRoot, projectID, "task-claims.json")
}

func (s *Service) loadClaims(projectID string) ([]Claim, error) {
	data, err := os.ReadFile(s.claimsPath(projectID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var claims []Claim
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// ListClaims returns every active claim for a project.
func (s *Service) ListClaims(projectID string) ([]Claim, error) {
	return s.loadClaims(projectID)
}

// Claim records that executorID/terminalID is now working taskID, replacing
// any earlier claim that terminal held on a different task.
func (s *Service) Claim(projectID, taskID, executorID, terminalID string) ([]Claim, error) {
	lock := s.lockFor("claims:" + projectID)
	lock.Lock()
	defer lock.Unlock()

	claims, err := s.loadClaims(projectID)
	if err != nil {
		return nil, err
	}
	kept := claims[:0:0]
	for _, c := range claims {
		if c.TerminalID == terminalID {
			continue
		}
		kept = append(kept, c)
	}
	kept = append(kept, Claim{TaskID: taskID, ExecutorID: executorID, TerminalID: terminalID, ClaimedAt: time.Now().UTC()})
	if err := atomicfile.WriteJSON(s.claimsPath(projectID), kept); err != nil {
		return nil, err
	}
	return kept, nil
}

// ReleaseClaim drops whatever claim terminalID currently holds.
func (s *Service) ReleaseClaim(projectID, terminalID string) ([]Claim, error) {
	lock := s.lockFor("claims:" + projectID)
	lock.Lock()
	defer lock.Unlock()

	claims, err := s.loadClaims(projectID)
	if err != nil {
		return nil, err
	}
	kept := claims[:0:0]
	for _, c := range claims {
		if c.TerminalID != terminalID {
			kept = append(kept, c)
		}
	}
	if err := atomicfile.WriteJSON(s.claimsPath(projectID), kept); err != nil {
		return nil, err
	}
	return kept, nil
}
