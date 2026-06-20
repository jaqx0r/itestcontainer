//go:build !linux

package main

import (
	"context"
	"fmt"

	"github.com/jaqx0r/itestcontainer/internal/runtime"
)

// newContainerdRuntime returns an error on non-Linux platforms where containerd is unavailable.
func newContainerdRuntime(_ context.Context) (runtime.Runtime, error) {
	return nil, fmt.Errorf("containerd runtime is only supported on Linux")
}
