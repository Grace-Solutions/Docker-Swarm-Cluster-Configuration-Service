package nodeagent

import (
	"context"
	"errors"
)

type JoinOptions struct {
	MasterAddr       string
	Role             string
	IPOverride       string
	HostnameOverride string
	OverlayProvider  string
	OverlayConfig    string
	EnableGluster    bool
}

var (
	// ErrNotImplemented is returned by functions that are not yet implemented.
	ErrNotImplemented = errors.New("nodeagent: not implemented")
)

// Join implements the node-side behaviour for `clusterctl node join`.
//
// In this initial scaffold it only validates basic options; the full
// implementation (registration handshake, Swarm/Gluster convergence, overlay
// setup, retry loop, etc.) will be added in later commits.
func Join(ctx context.Context, opts JoinOptions) error {
	_ = ctx
	if opts.MasterAddr == "" {
		return errors.New("master address is required")
	}
	return ErrNotImplemented
}

