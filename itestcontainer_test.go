package main

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func TestRunWithContextTerminatesContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cfg := Config{
		Name:      "alpine:latest",
		Cmd:       "sleep infinity",
		Labels:    "itestcontainer-test=TestRunWithContextTerminatesContainer",
		EnvLookup: func(s string) (string, bool) { return "", false },
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runWithContext(ctx, cancel, cfg)
	}()

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer dockerClient.Close()

	started := make(chan struct{})
	deadline := time.Now().Add(30 * time.Second)
	var containerID string
	for time.Now().Before(deadline) {
		result, err := dockerClient.ContainerList(context.Background(), client.ContainerListOptions{
			Filters: make(client.Filters).Add("label", "itestcontainer-test=TestRunWithContextTerminatesContainer"),
		})
		if err != nil {
			t.Fatalf("ContainerList: %v", err)
		}
		if len(result.Items) > 0 && result.Items[0].State == container.StateRunning {
			containerID = result.Items[0].ID
			close(started)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	select {
	case <-started:
	default:
		t.Fatal("container did not start within 30s")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("runWithContext returned error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runWithContext did not return within 30s after cancel")
	}

	result, err := dockerClient.ContainerList(context.Background(), client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", "itestcontainer-test=TestRunWithContextTerminatesContainer"),
	})
	if err != nil {
		t.Fatalf("ContainerList: %v", err)
	}
	if len(result.Items) != 0 {
		t.Errorf("container still exists after cancel: %v", containerID)
	}
}
