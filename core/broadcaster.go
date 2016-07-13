package core

import "github.com/jonmorehouse/gatekeeper/gatekeeper"

type EventCh chan Event

// Event is an interface, that wraps a gatekeeper.Event with additional information
// for internal purposes. Specifically only _some_ types of data are accessible
// through an event and we tightly control that via the interface.
type Event interface {
	Type() gatekeeper.Event
}

type UpstreamEvent struct {
	Event gatekeeper.Event

	Upstream   *gatekeeper.Upstream
	UpstreamID gatekeeper.UpstreamID
	Backend    *gatekeeper.Backend
	BackendID  gatekeeper.BackendID
}

func (u *UpstreamEvent) Type() gatekeeper.Event {
	return u.Event
}

func (u *UpstreamEvent) UpstreamEvent() (*UpstreamEvent, error) {
	validEvents := map[gatekeeper.Event]struct{}{
		gatekeeper.UpstreamAddedEvent:   struct{}{},
		gatekeeper.UpstreamRemovedEvent: struct{}{},
		gatekeeper.BackendAddedEvent:    struct{}{},
		gatekeeper.BackendRemovedEvent:  struct{}{},
	}

	if _, ok := validEvents[u.Event]; !ok {
		return nil, InvalidEventError
	}

	return u, nil
}

type ListenerID string

type Broadcaster interface {
	// Add a listener accepting all events of this type on the input channel
	AddListener(EventCh, []gatekeeper.Event) ListenerID

	// RemoveListener accepts a ListenerID and will remove it from
	// receiving messages. This does nothing to close the channel
	RemoveListener(ListenerID)

	// Publish is used to emit a message to any and all listeners for the
	// given type.
	Publish(Event)
}

func NewBroadcaster() Broadcaster {
	return &broadcaster{
		eventListeners: make(map[gatekeeper.Event]map[ListenerID]EventCh),
	}
}

type broadcaster struct {
	eventListeners map[gatekeeper.Event]map[ListenerID]EventCh
}

func (b *broadcaster) AddListener(ch EventCh, events []gatekeeper.Event) ListenerID {
	listenerID := ListenerID(gatekeeper.GetUUID())

	for _, event := range events {
		_, found := b.eventListeners[event]
		if !found {
			b.eventListeners[event] = make(map[ListenerID]EventCh, 1)
		}

		b.eventListeners[event][listenerID] = ch
	}

	return listenerID
}

func (b *broadcaster) RemoveListener(id ListenerID) {
	for event, _ := range b.eventListeners {
		delete(b.eventListeners[event], id)
	}
}

func (b *broadcaster) Publish(event Event) {
	listeners, ok := b.eventListeners[event.Type()]
	if !ok {
		return
	}

	for _, eventCh := range listeners {
		go func(EventCh) {
			eventCh <- event
		}(eventCh)

	}
}
