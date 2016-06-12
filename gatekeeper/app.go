package gatekeeper

import (
	"fmt"
	"sync"
	"time"

	"github.com/jonmorehouse/gatekeeper/shared"
)

type startStop interface {
	Start() error
	Stop(time.Duration) error
}

type App struct {
	// the server type adheres to the startStop interface, by convenience.
	servers []Server

	broadcaster       EventBroadcaster
	upstreamPublisher *UpstreamPublisher
	upstreamRequester UpstreamRequester
	loadBalancer      LoadBalancer
}

func New(options Options) (*App, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	// the broadcaster is what glues everything together. It is responsible
	// for dispensing events throughout the server so that plugins can
	// update themselves in accordance with systems going online and
	// offline.
	broadcaster := NewUpstreamEventBroadcaster()

	// each UpstreamPlugin is special because it is responsible for calling
	// asynchronously back into the parent process. Specifically it
	// requires an UpstreamPublisher which is cast as an
	// upstream_plugin.Manager to be accessible for calling back into the
	// parent program.
	upstreamPlugins := make([]PluginManager, 0, len(options.UpstreamPlugins))
	for _, pluginCmd := range options.UpstreamPlugins {
		plugin := NewPluginManager(pluginCmd, options.UpstreamPluginOpts, options.UpstreamPluginsCount, UpstreamPlugin)
		upstreamPlugins = append(upstreamPlugins, plugin)
	}
	// the upstreamPublisher needs to know about each pluginManager and in
	// return, each upstreamPlugin needs to use the UpstreamPublisher
	// because it implements the Manager interface and is what the
	// RPCServer that is launched inside of each RPCClient uses to emit
	// messages too
	upstreamPublisher := NewUpstreamPublisher(upstreamPlugins, broadcaster)
	// rpcUpstreamPublisher is an implementation of the upstreamPublisher
	// that is operable via RPC. The primary difference being that it uses
	// `shared.Error` instead of error interfaces
	rpcUpstreamPublisher := &RPCUpstreamPublisher{upstreamPublisher}

	// when the upstream plugins are configured, the publisher gets passed
	// to them and used as the manager type. This allows the upstreamPlugin
	// to talk back into this parent process.
	options.UpstreamPluginOpts["manager"] = rpcUpstreamPublisher

	// build an upstreamRequester for each server to communicate to the
	// upstream store. This is used to find the correct upstream for each
	// request.
	upstreamRequester := NewAsyncUpstreamRequester(broadcaster)

	// only one loadbalancer plugin is permitted, this is to ensure that we
	// actually have sane load balancing! Otherwise, we run the risk of
	// having multiple different load balancing algorithms at once.
	loadBalancerPlugin := NewPluginManager(options.LoadBalancerPlugin, options.LoadBalancerPluginOpts, options.LoadBalancerPluginsCount, LoadBalancerPlugin)
	loadBalancer := NewLoadBalancer(broadcaster, loadBalancerPlugin)

	// RequestModifier types are used to allow for requestModification via
	// plugin.
	// NOTE: this currently doesn't respect the ModifierPlugin options
	// because no such plugins currently exist.
	requestModifier := &LocalRequestModifier{}

	// ResponseModifier types are used to allow Responses to be modified
	// over RPC, allowing for users to handle responses as they'd like.
	// NOTE: this currently doesn't respect the ResponseModifier options
	// because no such plugins currently exist.
	responseModifier := &LocalResponseModifier{}

	// build out each server type
	servers := make([]Server, 0, 4)
	if options.HTTPPublicPort != 0 {
		servers = append(servers, &ProxyServer{
			port:              options.HTTPPublicPort,
			protocol:          shared.HTTPPublic,
			upstreamRequester: upstreamRequester,
			loadBalancer:      loadBalancer,
			requestModifier:   requestModifier,
			responseModifier:  responseModifier,
		})
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("at least one server must be specified")
	}

	return &App{
		broadcaster:       broadcaster,
		upstreamRequester: upstreamRequester,
		upstreamPublisher: upstreamPublisher,
		loadBalancer:      loadBalancer,
		servers:           servers,
	}, nil
}

func (a *App) Start() error {
	// start the upstreamRequester and loadBalancer first because they
	// receive notifications from the broadcaster immediately and we'd like
	// to make sure that any plugin that emits upstreams/backends to the
	// server at any time is supported. eg: if a plugin emits
	// upstreams/backends at start time and never again.
	syncStart := []startStop{
		a.upstreamRequester,
		a.loadBalancer,
		a.upstreamPublisher,
	}
	for _, job := range syncStart {
		if err := job.Start(); err != nil {
			return err
		}
	}

	// start all servers asynchronously
	var wg sync.WaitGroup
	errs := NewAsyncMultiError()
	for _, server := range a.servers {
		wg.Add(1)
		go func(s startStop) {
			defer wg.Done()
			if err := s.Start(); err != nil {
				errs.Add(err)
			}
		}(server)
	}

	wg.Wait()
	return errs.ToErr()
}

func (a *App) Stop(duration time.Duration) error {
	errs := NewAsyncMultiError()
	var wg sync.WaitGroup

	// stop accepting connections on each server first, and then start the
	// shutdown process. Its expected that the shutdown process takes
	// longer and as such, it is fired off in a goroutine at the same time
	// that other services throughout the app are shutdown.
	for _, server := range a.servers {
		if err := server.StopAccepting(); err != nil {
			errs.Add(err)
			continue
		}
		wg.Add(1)
		go func(s startStop) {
			defer wg.Done()
			if err := s.Stop(duration); err != nil {
				errs.Add(err)
			}
		}(server)
	}

	// shutdown all other plugins and internal subscribers
	jobs := []startStop{
		a.upstreamRequester,
		a.loadBalancer,
		a.upstreamPublisher,
	}
	for _, job := range jobs {
		wg.Add(1)
		go func(j startStop) {
			defer wg.Done()
			if err := j.Stop(duration); err != nil {
				errs.Add(err)
			}
		}(job)
	}

	wg.Wait()
	return errs.ToErr()
}