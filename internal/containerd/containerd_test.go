//go:build linux

package containerd

import (
	"context"
	"strings"
	"testing"

	"github.com/jaqx0r/itestcontainer/internal/runtime"
)


func TestRun_InvalidImage_NoPull(t *testing.T) {
	ctx := context.Background()
	r, err := New(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize ContainerdRuntime: %v", err)
	}
	defer r.Close()

	opts := runtime.RunOptions{
		Image:      "non-existent-image-name-12345",
		PullImages: false,
	}

	_, err = r.Run(ctx, opts)
	if err == nil {
		t.Error("Expected error when running non-existent image with PullImages: false, but got none")
	}
}

func TestRun_InvalidImage_WithPull(t *testing.T) {
	ctx := context.Background()
	r, err := New(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize ContainerdRuntime: %v", err)
	}
	defer r.Close()

	// Use an image name that is structurally valid but unlikely to exist.
	// If PullImages is true, we expect the containerd client to attempt
	// to contact the registry.
	opts := runtime.RunOptions{
		Image:      "docker.io/library/non-existent-image-abc-123",
		PullImages: true,
	}

	_, err = r.Run(ctx, opts)

	// We expect this to fail, but because of registry failure,
	// NOT because of a local image lookup failure.
	if err == nil {
		t.Fatal("Expected error when running non-existent image with PullImages: true, but got none")
	}

	// Verify the error is NOT "image not found" from local lookup.
	// The local lookup error comes from client.GetImage.
	// The pull error comes from client.Pull.
	if strings.Contains(err.Error(), "image") && strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected pull error, but got local lookup error: %v", err)
	}
}
