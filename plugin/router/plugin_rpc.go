package router

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
	"github.com/jonmorehouse/gatekeeper/gatekeeper"
	"github.com/jonmorehouse/gatekeeper/internal"
)

type AddUpstreamArgs struct {
	Upstream *gatekeeper.Upstream
}
type AddUpstreamResp struct {
	Err *gatekeeper.Error
}

type RemoveUpstreamArgs struct {
	UpstreamID gatekeeper.UpstreamID
}
type RemoveUpstreamResp struct {
	Err *gatekeeper.Error
}

type RouteRequestArgs struct {
	Req *gatekeeper.Request
}
type RouteRequestResp struct {
	Upstream *gatekeeper.Upstream
	Req      *gatekeeper.Request
	Err      *gatekeeper.Error
}

// PluginRPC is a representation of the Plugin interface that is RPC safe. It
// embeds an internal.BasePluginRPC which handles the basic RPC client
// communications of the `Start`, `Stop`, `Configure` and `Heartbeat` methods.
type RPCClient struct {
	broker *plugin.MuxBroker
	client *rpc.Client

	*internal.BasePluginRPCClient
}

func (c *RPCClient) AddUpstream(upstream *gatekeeper.Upstream) *gatekeeper.Error {
	args := &AddUpstreamArgs{
		Upstream: upstream,
	}
	resp := &AddUpstreamResp{}

	if err := c.client.Call("Plugin.AddUpstream", args, resp); err != nil {
		return gatekeeper.NewError(err)
	}

	return resp.Err
}

func (c *RPCClient) RemoveUpstream(upstreamID gatekeeper.UpstreamID) *gatekeeper.Error {
	args := &RemoveUpstreamArgs{
		UpstreamID: upstreamID,
	}
	resp := &RemoveUpstreamResp{}

	if err := c.client.Call("Plugin.RemoveUpstream", args, resp); err != nil {
		return gatekeeper.NewError(err)
	}

	return resp.Err
}

func (c *RPCClient) RouteRequest(req *gatekeeper.Request) (*gatekeeper.Upstream, *gatekeeper.Request, *gatekeeper.Error) {
	args := &RouteRequestArgs{
		Req: req,
	}
	resp := &RouteRequestResp{}

	if err := c.client.Call("Plugin.RouteRequest", args, resp); err != nil {
		return nil, args.Req, gatekeeper.NewError(err)
	}

	return resp.Upstream, resp.Req, resp.Err

}

type RPCServer struct {
	impl   Plugin
	broker *plugin.MuxBroker

	*internal.BasePluginRPCServer
}

func (s *RPCServer) AddUpstream(args *AddUpstreamArgs, resp *AddUpstreamResp) error {
	if err := s.impl.AddUpstream(args.Upstream); err != nil {
		resp.Err = gatekeeper.NewError(err)
	}
	return nil
}

func (s *RPCServer) RemoveUpstream(args *RemoveUpstreamArgs, resp *RemoveUpstreamResp) error {
	if err := s.impl.RemoveUpstream(args.UpstreamID); err != nil {
		resp.Err = gatekeeper.NewError(err)
	}
	return nil
}

func (s *RPCServer) RouteRequest(args *RouteRequestArgs, resp *RouteRequestResp) error {
	upstream, req, err := s.impl.RouteRequest(args.Req)
	if err != nil {
		resp.Err = gatekeeper.NewError(err)
		return nil
	}
	resp.Upstream = upstream
	resp.Req = req
	return nil
}
