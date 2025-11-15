package controller

import (
	"context"
	"errors"
	"os"
	"time"

	"clusterctl/internal/logging"
)

type ServeOptions struct {
	ListenAddr     string
	StateDir       string
	AdvertiseAddr  string
	MinManagers    int
	MinWorkers     int
	WaitForMinimum bool
}

type MasterInitOptions struct {
	StateDir      string
	EnableGluster bool
}

type MasterResetOptions struct {
	StateDir string
}

type NodeRegistration struct {
	Hostname       string    `json:"hostname"`
	Role           string    `json:"role"`
	IP             string    `json:"ip"`
	OS             string    `json:"os"`
	CPU            int       `json:"cpu"`
	MemoryMB       int       `json:"memoryMb"`
	DockerVersion  string    `json:"dockerVersion"`
	GlusterCapable bool      `json:"glusterCapable"`
	Timestamp      time.Time `json:"timestamp"`
	// Action controls how the controller treats this registration. If empty or
	// "register", the node is upserted into state. If "deregister", the node
	// is removed from state.
	Action string `json:"action,omitempty"`
}

type NodeResponseStatus string

const (
	StatusReady   NodeResponseStatus = "ready"
	StatusWaiting NodeResponseStatus = "waiting"
)

type NodeResponse struct {
	Status           NodeResponseStatus `json:"status"`
	SwarmRole        string             `json:"swarmRole"`
	SwarmJoinToken   string             `json:"swarmJoinToken"`
	SwarmManagerAddr string             `json:"swarmManagerAddr"`
	OverlayType      string             `json:"overlayType"`
	OverlayPayload   string             `json:"overlayPayload"`
	GlusterEnabled   bool               `json:"glusterEnabled"`
	GlusterVolume    string             `json:"glusterVolume"`
	GlusterMount     string             `json:"glusterMount"`
	GlusterBrick     string             `json:"glusterBrick"`
}

// MasterInit prepares a host as the initial Swarm manager and optional GlusterFS brick.
//
// For now this ensures that the controller state directory exists and records a
// structured log entry. Swarm and GlusterFS orchestration are added in
// subsequent commits.
func MasterInit(ctx context.Context, opts MasterInitOptions) error {
	_ = ctx // reserved for future use (shelling out, etc.)

	if opts.StateDir == "" {
		return errors.New("controller: state dir must be set")
	}

	if err := os.MkdirAll(opts.StateDir, 0o700); err != nil {
		return err
	}

	logging.L().Infow("master init complete",
		"stateDir", opts.StateDir,
		"enableGluster", opts.EnableGluster,
	)

	return nil
}

// MasterReset clears the controller's persisted state. It is safe to run
// multiple times; after reset the controller behaves as if no nodes have
// registered yet.
func MasterReset(ctx context.Context, opts MasterResetOptions) error {
	_ = ctx // reserved for potential future orchestration work

	if opts.StateDir == "" {
		return errors.New("controller: state dir must be set")
	}

	store, err := newFileStore(opts.StateDir)
	if err != nil {
		return err
	}

	if _, err := store.reset(); err != nil {
		return err
	}

	logging.L().Infow("master reset complete", "stateDir", opts.StateDir)
	return nil
}
