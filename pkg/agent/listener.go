package agent

import (
	"github.com/vince-0202/acgo/pkg/communi"
	"sync"
)

// listenerSlot holds a listener and an id so Subscribe can return a working unsub.
type listenerSlot struct {
	id int
	l  communi.Listener
}

type listenerManager struct {
	listenersMu    sync.RWMutex
	listeners      []listenerSlot
	nextListenerID int
}

// AddListener registers a listener for events. It returns an unsubscribe function.
func (lm *listenerManager) AddListener(l communi.Listener) func() {
	lm.listenersMu.Lock()
	defer lm.listenersMu.Unlock()
	id := lm.nextListenerID
	lm.nextListenerID++
	lm.listeners = append(lm.listeners, listenerSlot{id: id, l: l})
	return func() {
		lm.listenersMu.Lock()
		defer lm.listenersMu.Unlock()
		for i := range lm.listeners {
			if lm.listeners[i].id == id {
				last := len(lm.listeners) - 1
				if i != last {
					lm.listeners[i] = lm.listeners[last]
				}
				lm.listeners = lm.listeners[:last]
				return
			}
		}
	}
}

func (lm *listenerManager) emit(e communi.AgentEvent) {
	lm.listenersMu.RLock()
	defer lm.listenersMu.RUnlock()
	for _, slot := range lm.listeners {
		slot.l(e)
	}
}
