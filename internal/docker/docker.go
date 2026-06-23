package docker

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/jaqx0r/itestcontainer/internal/runtime"
	mobycontainer "github.com/moby/moby/api/types/container"
	mobymount "github.com/moby/moby/api/types/mount"
	mobynetwork "github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
	"github.com/moby/moby/api/pkg/stdcopy"
)

// DockerRuntime implements runtime.Runtime using the moby Docker client.
type DockerRuntime struct {
	client *mobyclient.Client
}

// New creates a DockerRuntime using environment-configured Docker connection.
func New(_ context.Context) (*DockerRuntime, error) {
	c, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &DockerRuntime{client: c}, nil
}

// Close releases the underlying Docker client.
func (r *DockerRuntime) Close() error {
	return r.client.Close()
}

// Run pulls the image, creates, and starts a container with the given options.
func (r *DockerRuntime) Run(ctx context.Context, opts runtime.RunOptions) (runtime.Container, error) {
	if opts.PullImages {
		if err := r.pullImage(ctx, opts.Image); err != nil {
			return nil, fmt.Errorf("pull %s: %w", opts.Image, err)
		}
	} else {
		if _, err := r.client.ImageInspect(ctx, opts.Image); err != nil {
			return nil, fmt.Errorf("image %s not found: %w", opts.Image, err)
		}
	}

	containerConfig, err := r.buildContainerConfig(opts)
	if err != nil {
		return nil, err
	}
	hostConfig, err := r.buildHostConfig(opts)
	if err != nil {
		return nil, err
	}

	result, err := r.client.ContainerCreate(ctx, mobyclient.ContainerCreateOptions{
		Config:     containerConfig,
		HostConfig: hostConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("ContainerCreate: %w", err)
	}
	id := result.ID

	if _, err := r.client.ContainerStart(ctx, id, mobyclient.ContainerStartOptions{}); err != nil {
		_, _ = r.client.ContainerRemove(context.Background(), id, mobyclient.ContainerRemoveOptions{Force: true})
		return nil, fmt.Errorf("ContainerStart: %w", err)
	}

	c := &dockerContainer{client: r.client, id: id}

	if opts.LogLine != nil {
		go c.streamLogs(opts.LogLine)
	}

	return c, nil
}

func (r *DockerRuntime) pullImage(ctx context.Context, image string) error {
	_, err := r.client.ImageInspect(ctx, image)
	if err == nil {
		return nil // image exists locally, skip pull
	}
	resp, err := r.client.ImagePull(ctx, image, mobyclient.ImagePullOptions{})
	if err != nil {
		return err
	}
	return resp.Wait(ctx)
}

func (r *DockerRuntime) buildContainerConfig(opts runtime.RunOptions) (*mobycontainer.Config, error) {
	exposedPorts := make(mobynetwork.PortSet, len(opts.ExposedPorts))
	for _, p := range opts.ExposedPorts {
		port, err := mobynetwork.ParsePort(p)
		if err != nil {
			return nil, fmt.Errorf("invalid exposed port %q: %w", p, err)
		}
		exposedPorts[port] = struct{}{}
	}

	var env []string
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}

	cfg := &mobycontainer.Config{
		Image:        opts.Image,
		Env:          env,
		ExposedPorts: exposedPorts,
		Labels:       opts.Labels,
	}
	if len(opts.Cmd) > 0 {
		cfg.Cmd = opts.Cmd
	}
	return cfg, nil
}

func (r *DockerRuntime) buildHostConfig(opts runtime.RunOptions) (*mobycontainer.HostConfig, error) {
	portBindings := make(mobynetwork.PortMap, len(opts.PortBindings))
	for port, bindings := range opts.PortBindings {
		mobyPort, err := mobynetwork.ParsePort(string(port))
		if err != nil {
			return nil, fmt.Errorf("invalid port binding key %q: %w", port, err)
		}
		var mobyBindings []mobynetwork.PortBinding
		for _, b := range bindings {
			mobyBindings = append(mobyBindings, mobynetwork.PortBinding{
				HostIP:   b.HostIP,
				HostPort: b.HostPort,
			})
		}
		portBindings[mobyPort] = mobyBindings
	}

	var mounts []mobymount.Mount
	for _, m := range opts.Mounts {
		switch m.Type {
		case runtime.MountTypeVolume:
			mounts = append(mounts, mobymount.Mount{
				Type:   mobymount.TypeVolume,
				Source: m.Source,
				Target: m.Target,
			})
		case runtime.MountTypeBind:
			mounts = append(mounts, mobymount.Mount{
				Type:   mobymount.TypeBind,
				Source: m.Source,
				Target: m.Target,
			})
		}
	}

	return &mobycontainer.HostConfig{
		PortBindings: portBindings,
		Mounts:       mounts,
	}, nil
}

// dockerContainer is a running Docker container handle.
type dockerContainer struct {
	client *mobyclient.Client
	id     string
}

func (c *dockerContainer) ID() string { return c.id }

// Stop stops and removes the container.
func (c *dockerContainer) Stop(ctx context.Context) error {
	_, stopErr := c.client.ContainerStop(ctx, c.id, mobyclient.ContainerStopOptions{})
	_, removeErr := c.client.ContainerRemove(ctx, c.id, mobyclient.ContainerRemoveOptions{
		RemoveVolumes: false,
		Force:         true,
	})
	if stopErr != nil {
		return fmt.Errorf("ContainerStop: %w", stopErr)
	}
	if removeErr != nil {
		return fmt.Errorf("ContainerRemove: %w", removeErr)
	}
	return nil
}

// streamLogs attaches to container logs and calls logLine for each line.
// Uses stdcopy to correctly demultiplex the Docker non-TTY frame format.
func (c *dockerContainer) streamLogs(logLine func(stream, line string)) {
	result, err := c.client.ContainerLogs(context.Background(), c.id, mobyclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return
	}
	defer result.Close()

	stdcopy.StdCopy(
		newLineWriter("stdout", logLine),
		newLineWriter("stderr", logLine),
		result,
	)
}

// lineWriter splits incoming bytes on newlines and calls logLine per line.
type lineWriter struct {
	stream  string
	logLine func(stream, line string)
	buf     []byte
}

func newLineWriter(stream string, logLine func(stream, line string)) *lineWriter {
	return &lineWriter{stream: stream, logLine: logLine}
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:idx]), "\r")
		w.logLine(w.stream, line)
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}
