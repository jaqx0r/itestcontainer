package runtime

import (
	"context"
	"net/netip"
)

// RuntimeName identifies a container runtime backend.
type RuntimeName string

const (
	RuntimeDocker     RuntimeName = "docker"
	RuntimeContainerd RuntimeName = "containerd"
)

// Port is a container port spec e.g. "80/tcp".
type Port string

// PortBinding maps a container port to a host port.
type PortBinding struct {
	HostIP   netip.Addr
	HostPort string
}

// MountType represents the type of a mount.
type MountType string

const (
	MountTypeBind   MountType = "bind"
	MountTypeVolume MountType = "volume"
)

// Mount is a container volume mount.
type Mount struct {
	Type   MountType
	Source string // volume name or host path
	Target string // path inside container
}

// RunOptions are the options for running a container.
type RunOptions struct {
	Image        string
	Cmd          []string
	Env          map[string]string
	ExposedPorts []string               // e.g. ["80/tcp"]
	PortBindings map[Port][]PortBinding
	Mounts       []Mount
	Labels       map[string]string
	LogLine      func(stream, line string) // nil = no logging
}

// Container is a running container handle.
type Container interface {
	ID() string
	Stop(ctx context.Context) error
}

// Runtime launches and manages containers.
type Runtime interface {
	Run(ctx context.Context, opts RunOptions) (Container, error)
	Close() error
}
