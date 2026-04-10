package agent

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vince-0202/acgo/pkg/utils"
)

var (
	errMessageRouteDenied = errors.New("message route denied")
	errMessageTargetEmpty = errors.New("message target is empty")
)

type MessageEnvelope struct {
	ID            string         `json:"id"`
	FromSubID     string         `json:"from_sub_id"`
	ToSubID       string         `json:"to_sub_id"`
	Intent        string         `json:"intent,omitempty"`
	Payload       string         `json:"payload,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	AckedAt       *time.Time     `json:"acked_at,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (m MessageEnvelope) clone() MessageEnvelope {
	out := m
	if m.Metadata != nil {
		out.Metadata = make(map[string]any, len(m.Metadata))
		for k, v := range m.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

type messageBus struct {
	mu      sync.Mutex
	inbox   map[string][]MessageEnvelope
	byID    map[string]MessageEnvelope
	history []MessageEnvelope
}

func newMessageBus() *messageBus {
	return &messageBus{
		inbox:   make(map[string][]MessageEnvelope),
		byID:    make(map[string]MessageEnvelope),
		history: make([]MessageEnvelope, 0),
	}
}

func (b *messageBus) Send(msg MessageEnvelope) (MessageEnvelope, error) {
	to := strings.TrimSpace(msg.ToSubID)
	if to == "" {
		return MessageEnvelope{}, errMessageTargetEmpty
	}
	msg.ID = strings.TrimSpace(msg.ID)
	if msg.ID == "" {
		msg.ID = "msg-" + utils.SnowflakeIDString()
	}
	msg.FromSubID = strings.TrimSpace(msg.FromSubID)
	msg.ToSubID = to
	msg.Intent = strings.TrimSpace(msg.Intent)
	msg.CorrelationID = strings.TrimSpace(msg.CorrelationID)
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	stored := msg.clone()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inbox[stored.ToSubID] = append(b.inbox[stored.ToSubID], stored)
	b.byID[stored.ID] = stored
	b.history = append(b.history, stored)
	return stored.clone(), nil
}

func (b *messageBus) Pull(toSubID string, limit int, correlationID string) []MessageEnvelope {
	toSubID = strings.TrimSpace(toSubID)
	correlationID = strings.TrimSpace(correlationID)
	if toSubID == "" || limit == 0 {
		return nil
	}
	if limit < 0 {
		limit = 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	all := b.inbox[toSubID]
	if len(all) == 0 {
		return nil
	}
	out := make([]MessageEnvelope, 0, len(all))
	for _, m := range all {
		if correlationID != "" && m.CorrelationID != correlationID {
			continue
		}
		out = append(out, m.clone())
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (b *messageBus) Ack(messageID string) (MessageEnvelope, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return MessageEnvelope{}, fmt.Errorf("message_id is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	msg, ok := b.byID[messageID]
	if !ok {
		return MessageEnvelope{}, fmt.Errorf("message %s not found", messageID)
	}
	now := time.Now()
	msg.AckedAt = &now
	b.byID[messageID] = msg
	for to, list := range b.inbox {
		for i := range list {
			if list[i].ID == messageID {
				list[i] = msg
				b.inbox[to] = list
				break
			}
		}
	}
	for i := range b.history {
		if b.history[i].ID == messageID {
			b.history[i] = msg
			break
		}
	}
	return msg.clone(), nil
}

func (b *messageBus) ListByCorrelation(correlationID string) []MessageEnvelope {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]MessageEnvelope, 0)
	for _, m := range b.history {
		if m.CorrelationID == correlationID {
			out = append(out, m.clone())
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (b *messageBus) Recent(limit int) []MessageEnvelope {
	if limit <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.history)
	if n == 0 {
		return nil
	}
	if limit > n {
		limit = n
	}
	start := n - limit
	out := make([]MessageEnvelope, 0, limit)
	for i := start; i < n; i++ {
		out = append(out, b.history[i].clone())
	}
	return out
}
