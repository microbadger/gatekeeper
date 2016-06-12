package main

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	loadbalancer_plugin "github.com/jonmorehouse/gatekeeper/plugin/loadbalancer"
	"github.com/jonmorehouse/gatekeeper/shared"
)

// implements the loadbalancer.LoadBalancer plugin that is exposed over RPC
type LoadBalancer struct {
	sync.RWMutex

	upstreamBackends map[shared.UpstreamID][]shared.Backend
}

func NewLoadBalancer() *LoadBalancer {
	return &LoadBalancer{
		upstreamBackends: make(map[shared.UpstreamID][]shared.Backend),
	}
}

// no special configuration needed, but we implement these methods anyway for the interface
func (l *LoadBalancer) Start() *shared.Error {
	log.Println("simple-loadbalancer plugin started...")
	return nil
}
func (l *LoadBalancer) Stop() *shared.Error {
	log.Println("simple-loadbalancer plugin stopped...")
	return nil
}
func (l *LoadBalancer) Configure(opts map[string]interface{}) *shared.Error { return nil }
func (l *LoadBalancer) Heartbeat() *shared.Error                            { return nil }

// actual implementation of methods used
func (l *LoadBalancer) AddBackend(upstream shared.UpstreamID, backend shared.Backend) *shared.Error {
	log.Println(upstream, backend)
	// TODO: handle duplicate backends here
	if _, ok := l.upstreamBackends[upstream]; !ok {
		l.upstreamBackends[upstream] = make([]shared.Backend, 0, 1)
	}
	l.upstreamBackends[upstream] = append(l.upstreamBackends[upstream], backend)
	return nil
}

func (l *LoadBalancer) RemoveBackend(deleted shared.Backend) *shared.Error {
	found := false

	for upstream, backends := range l.upstreamBackends {
		for idx, backend := range backends {
			if backend != deleted {
				continue
			}

			found = true
			backends = append(backends[:idx], backends[idx+1:]...)
			l.upstreamBackends[upstream] = backends
			break
		}
	}

	if !found {
		return shared.NewError(fmt.Errorf("Did not find backend"))
	}
	return nil
}

func (l *LoadBalancer) GetBackend(upstream shared.UpstreamID) (shared.Backend, *shared.Error) {
	backends, found := l.upstreamBackends[upstream]
	if !found {
		return shared.NilBackend, shared.NewError(fmt.Errorf("UPSTREAM_NOT_FOUND"))
	} else if len(backends) == 0 {
		return shared.NilBackend, shared.NewError(fmt.Errorf("NO_BACKENDS_FOUND"))
	}

	// pick a random backend for this upstream and return it
	idx := rand.Intn(len(backends))
	return backends[idx], nil
}

func main() {
	rand.Seed(time.Now().Unix())
	loadBalancer := NewLoadBalancer()
	if err := loadbalancer_plugin.RunPlugin("simple-loadbalancer", loadBalancer); err != nil {
		log.Fatal(err)
	}
}