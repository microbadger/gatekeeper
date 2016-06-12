package loadbalancer

import (
	"net/rpc"
	"os/exec"

	"github.com/hashicorp/go-plugin"
	"github.com/jonmorehouse/gatekeeper/shared"
)

var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "gatekeeper|plugin-type",
	MagicCookieValue: "loadbalancer",
}

type Opts map[string]interface{}

// this is the interface that gatekeeper sees
type Plugin interface {
	// standard plugin methods
	Start() *shared.Error
	Stop() *shared.Error
	// this isn't Opts, because we want to make this as general as possible
	// for expressiveness between different plugins
	Configure(map[string]interface{}) *shared.Error
	// Heartbeat is called by a plugin manager in the primary application periodically
	Heartbeat() *shared.Error

	// loadbalancer specific methods
	AddBackend(shared.UpstreamID, shared.Backend) *shared.Error
	RemoveBackend(shared.Backend) *shared.Error
	GetBackend(shared.UpstreamID) (shared.Backend, *shared.Error)
}

type PluginDispenser struct {
	// this is the actual plugin's implementation of the plugin interface.
	// Everything in this package just proxies requests to this object.
	impl Plugin
}

func (d PluginDispenser) Server(b *plugin.MuxBroker) (interface{}, error) {
	return &RPCServer{broker: b, impl: d.impl}, nil
}

func (d PluginDispenser) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &RPCClient{broker: b, client: c}, nil
}

// This is the method that a plugin will call to start serving traffic over the
// plugin interface. Specifically, this will start the RPC server and register
// etc.
func RunPlugin(name string, impl Plugin) error {
	pluginDispenser := PluginDispenser{impl: impl}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{
			name: &pluginDispenser,
		},
	})
	return nil
}

func NewClient(name string, cmd string) (Plugin, func(), error) {
	pluginDispenser := PluginDispenser{}

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{
			name: &pluginDispenser,
		},
		Cmd: exec.Command(cmd),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, func() {}, err
	}

	rawPlugin, err := rpcClient.Dispense(name)
	if err != nil {
		client.Kill()
		return nil, func() {}, err
	}

	return rawPlugin.(Plugin), func() { client.Kill() }, nil
}