package gatekeeper

import (
	"sync"
	"time"

	"github.com/jonmorehouse/gatekeeper/shared"
)

type UpstreamMatcher interface {
	Start() error
	Stop(time.Duration) error

	Match(*shared.Request) (*shared.Upstream, shared.UpstreamMatchType, error)
}

type UpstreamMatcherClient interface {
	Match(*shared.Request) (*shared.Upstream, shared.UpstreamMatchType, error)
}

type upstreamMatcher struct {
	broadcaster EventBroadcaster
	listenID    EventListenerID
	listenCh    EventCh
	stopCh      chan struct{}

	knownUpstreams      map[shared.UpstreamID]*shared.Upstream
	upstreamsByHostname map[string]*shared.Upstream
	upstreamsByPrefix   map[string]*shared.Upstream
	sync.RWMutex
}

func NewUpstreamMatcher(broadcaster EventBroadcaster) UpstreamMatcher {
	return &upstreamMatcher{
		broadcaster: broadcaster,
		listenCh:    make(chan Event),
		stopCh:      make(chan struct{}),

		knownUpstreams:      make(map[shared.UpstreamID]*shared.Upstream),
		upstreamsByHostname: make(map[string]*shared.Upstream),
		upstreamsByPrefix:   make(map[string]*shared.Upstream),
	}
}

func (r *upstreamMatcher) Start() error {
	id, err := r.broadcaster.AddListener(r.listenCh, []EventType{UpstreamAdded, UpstreamRemoved})
	if err != nil {
		return err
	}

	r.listenID = id
	go r.listener()
	return nil
}

func (r *upstreamMatcher) listener() {
	for {
		select {
		case rawEvent := <-r.listenCh:
			upstreamEvent, ok := rawEvent.(UpstreamEvent)
			if !ok {
				shared.ProgrammingError(InternalEventError.String())
				continue
			}
			if upstreamEvent.Type() == UpstreamAdded {
				r.addUpstream(upstreamEvent)
			} else if upstreamEvent.Type() == UpstreamRemoved {
				r.addUpstream(upstreamEvent)
			} else {
				shared.ProgrammingError(InternalEventError.String())
			}
		case <-r.stopCh:
			r.stopCh <- struct{}{}
			return
		}
	}
}

func (r *upstreamMatcher) addUpstream(event UpstreamEvent) {
	if event.UpstreamID == shared.NilUpstreamID {
		shared.ProgrammingError(InternalEventError.String())
		return
	}
	r.Lock()
	defer r.Unlock()
	r.knownUpstreams[event.UpstreamID] = event.Upstream
}

func (r *upstreamMatcher) removeUpstream(event UpstreamEvent) {
	r.RLock()
	upstr, ok := r.knownUpstreams[event.UpstreamID]
	r.RUnlock()

	if !ok {
		shared.ProgrammingError(UpstreamNotFoundError.String())
		return
	}

	r.Lock()
	defer r.Unlock()

	for _, hostname := range upstr.Hostnames {
		if _, ok := r.upstreamsByHostname[hostname]; ok {
			delete(r.upstreamsByHostname, hostname)
		}
	}

	for _, prefix := range upstr.Prefixes {
		if _, ok := r.upstreamsByPrefix[prefix]; ok {
			delete(r.upstreamsByPrefix, prefix)
		}
	}
	delete(r.knownUpstreams, event.UpstreamID)
}

func (r *upstreamMatcher) Stop(duration time.Duration) error {
	r.broadcaster.RemoveListener(r.listenID)
	r.listenID = NilEventListenerID
	r.stopCh <- struct{}{}

	for {
		select {
		case <-r.stopCh:
			goto finish
		case <-time.After(duration):
			return InternalTimeoutError
		}
	}

finish:
	close(r.listenCh)
	close(r.stopCh)
	return nil
}

func (r *upstreamMatcher) Match(req *shared.Request) (*shared.Upstream, shared.UpstreamMatchType, error) {
	r.Lock()
	defer r.Unlock()

	hostname := req.Host
	prefix := req.Prefix

	// check hostname cache
	if upstream, ok := r.upstreamsByHostname[hostname]; hostname != "" && ok {
		return upstream, shared.HostnameUpstreamMatch, nil
	}

	// check prefix cache
	if upstream, ok := r.upstreamsByPrefix[prefix]; prefix != "" && ok {
		return upstream, shared.PrefixUpstreamMatch, nil
	}

	// check all knownUpstreams, returning the first match
	for _, upstream := range r.knownUpstreams {
		if upstream.HasHostname(hostname) {
			r.upstreamsByHostname[hostname] = upstream
			return upstream, shared.HostnameUpstreamMatch, nil
		}
		if upstream.HasPrefix(prefix) {
			r.upstreamsByPrefix[prefix] = upstream
			return upstream, shared.PrefixUpstreamMatch, nil
		}
	}

	return nil, shared.NilUpstreamMatch, UpstreamNotFoundError
}