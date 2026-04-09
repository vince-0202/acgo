package runtime

import (
	"context"
	"fmt"
	"github.com/vince-0202/acgo/pkg/log"
	"sort"
	"strings"
	"sync"
	"time"
)

type cronTask struct {
	ID              string
	AgentID         string
	Prompt          string
	CreatedAt       time.Time
	NextRunAt       time.Time
	LastRunAt       time.Time
	IntervalSeconds int
	MaxRuns         int
	RunCount        int
	Status          string
}

type cronManager struct {
	mu      sync.Mutex
	tasks   map[string]*cronTask
	started bool
	stopCh  chan struct{}
}

func newCronManager() *cronManager {
	return &cronManager{
		tasks:  make(map[string]*cronTask),
		stopCh: make(chan struct{}),
	}
}

func (m *cronManager) Create(agentID string, prompt string, delaySeconds int, intervalSeconds int, maxRuns int) *cronTask {
	if delaySeconds < 0 {
		delaySeconds = 0
	}
	if intervalSeconds < 0 {
		intervalSeconds = 0
	}
	if intervalSeconds == 0 && maxRuns <= 0 {
		maxRuns = 1
	}

	now := time.Now()
	task := &cronTask{
		ID:              fmt.Sprintf("cron-%d", now.UnixNano()),
		AgentID:         agentID,
		Prompt:          prompt,
		CreatedAt:       now,
		NextRunAt:       now.Add(time.Duration(delaySeconds) * time.Second),
		IntervalSeconds: intervalSeconds,
		MaxRuns:         maxRuns,
		Status:          "scheduled",
	}
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()
	return task
}

func (m *cronManager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[id]; !ok {
		return false
	}
	delete(m.tasks, id)
	return true
}

func (m *cronManager) List() []cronTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]cronTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (m *cronManager) Start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	defer m.mu.Unlock()
	go m.loop()
}

func (m *cronManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return
	}
	close(m.stopCh)
	m.started = false
}

func (m *cronManager) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.runDueTasks()
		case <-m.stopCh:
			return
		}
	}
}

func (m *cronManager) runDueTasks() {
	type dueRun struct {
		taskID  string
		agentID string
		prompt  string
	}
	runs := make([]dueRun, 0)
	m.mu.Lock()
	now := time.Now()
	for _, t := range m.tasks {
		if t.Status != "scheduled" {
			continue
		}
		if t.NextRunAt.After(now) {
			continue
		}
		runs = append(runs, dueRun{taskID: t.ID, agentID: t.AgentID, prompt: t.Prompt})
		t.RunCount++
		t.LastRunAt = now
		if t.IntervalSeconds <= 0 {
			t.Status = "completed"
			continue
		}
		if t.MaxRuns > 0 && t.RunCount >= t.MaxRuns {
			t.Status = "completed"
			continue
		}
		t.NextRunAt = now.Add(time.Duration(t.IntervalSeconds) * time.Second)
	}
	m.mu.Unlock()

	for _, r := range runs {
		ag, ok := GetAgent(r.agentID)
		if !ok || ag == nil {
			log.Debugf("[cron] skip run task=%s agent=%s: agent not found", r.taskID, r.agentID)
			continue
		}
		go func(taskID, agentID, prompt string) {
			taskPrompt := strings.TrimSpace(prompt)
			log.Debugf("[cron] dispatch scheduled task=%s agent=%s", taskID, agentID)
			if err := ag.PromptScheduledTask(context.Background(), taskPrompt); err != nil {
				log.Debugf("[cron] task=%s agent=%s prompt failed: %v", taskID, agentID, err)
				return
			}
			log.Debugf("[cron] task=%s agent=%s prompt executed", taskID, agentID)
		}(r.taskID, r.agentID, r.prompt)
	}
}

var DefaultCronManager = newCronManager()

func init() {
	DefaultCronManager.Start()
}
