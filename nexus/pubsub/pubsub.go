package pubsub

import (
	"sync"
)

type EventHandler func(payload interface{})

type PubSub struct {
	mutex       sync.RWMutex
	subscribers map[string][]EventHandler
}

func NewPubSub() *PubSub {
	return &PubSub{
		subscribers: make(map[string][]EventHandler),
	}
}

// Registers an event handler for a specific event
func (ps *PubSub) Subscribe(event string, handler EventHandler) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	ps.subscribers[event] = append(ps.subscribers[event], handler)
}

// Publish emits an event to all registered handlers
func (ps *PubSub) Publish(event string, payload interface{}) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	if handlers, found := ps.subscribers[event]; found {
		for _, handler := range handlers {
			go handler(payload)
		}
	}
}

func (ps *PubSub) HasSubscribers(event string) bool {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	_, ok := ps.subscribers[event]
	return ok
}
