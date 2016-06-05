package upstream

import (
	"github.com/jonmorehouse/gatekeeper/shared"
)

// manager is the interface which clients use to talk back to the gatekeeper parent process
type Manager interface {
	AddUpstream(shared.Upstream) (shared.UpstreamID, error)
	RemoveUpstream(shared.UpstreamID) error

	AddBackend(shared.UpstreamID, shared.Backend) (shared.BackendID, error)
	RemoveBackend(shared.BackendID) error
}
