package agent

import (
	"github.com/vince-0202/acgo/pkg/communi"
	"sync"
)

type QueueManager struct {
	queueMu sync.Mutex // protects SteeringQueue and FollowUpQueue

	// SteeringQueue holds user/steering messages to process next; consumed before FollowUpQueue.
	// When the agent is busy, callers may enqueue here; after the current turn ends, these are processed first.
	SteeringQueue []communi.Message

	// FollowUpQueue holds follow-up messages; consumed after SteeringQueue is empty.
	FollowUpQueue []communi.Message
}

func (qm *QueueManager) Clean() {
	qm.queueMu.Lock()
	qm.SteeringQueue = nil
	qm.FollowUpQueue = nil
	qm.queueMu.Unlock()
}

func (qm *QueueManager) EnqueueSteering(msg communi.Message) {
	qm.queueMu.Lock()
	defer qm.queueMu.Unlock()
	qm.SteeringQueue = append(qm.SteeringQueue, msg)
}

func (qm *QueueManager) EnqueueFollowUp(msg communi.Message) {
	qm.queueMu.Lock()
	defer qm.queueMu.Unlock()
	qm.FollowUpQueue = append(qm.FollowUpQueue, msg)
}

// DrainOneFromQueues removes and returns one message: steering first, then follow-up.
// Caller must not hold queueMu.
func (qm *QueueManager) DrainOneFromQueues() *communi.Message {
	qm.queueMu.Lock()
	defer qm.queueMu.Unlock()
	if len(qm.SteeringQueue) > 0 {
		msg := qm.SteeringQueue[0]
		qm.SteeringQueue = qm.SteeringQueue[1:]
		return &msg
	}
	if len(qm.FollowUpQueue) > 0 {
		msg := qm.FollowUpQueue[0]
		qm.FollowUpQueue = qm.FollowUpQueue[1:]
		return &msg
	}
	return nil
}
