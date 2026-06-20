//go:build linux

package main

import (
	"context"

	"github.com/jaqx0r/itestcontainer/internal/containerd"
	"github.com/jaqx0r/itestcontainer/internal/runtime"
)

// newContainerdRuntime creates a ContainerdRuntime on Linux.
func newContainerdRuntime(ctx context.Context) (runtime.Runtime, error) {
	return containerd.New(ctx)
}
