//go:build linux

package containerd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	containerdclient "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	gocni "github.com/containerd/go-cni"
	"github.com/jaqx0r/itestcontainer/internal/runtime"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	containerdSocket    = "/run/containerd/containerd.sock"
	itestNamespace      = "itestcontainer"
	volumeBaseDir       = "/var/lib/itestcontainer/volumes"
)

// bridgeCNIConfig is embedded so no external CNI config files are needed.
var bridgeCNIConfig = []byte(`{
  "cniVersion": "0.4.0",
  "name": "itestcontainer-bridge",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "itc0",
      "isGateway": true,
      "ipMasq": true,
      "ipam": {
        "type": "host-local",
        "subnet": "10.88.0.0/16",
        "routes": [{"dst": "0.0.0.0/0"}]
      }
    },
    {
      "type": "portmap",
      "capabilities": {"portMappings": true}
    }
  ]
}`)

// containerdClient is the subset of containerdclient.Client methods used by ContainerdRuntime.
type containerdClient interface {
	GetImage(ctx context.Context, ref string) (containerdclient.Image, error)
	Pull(ctx context.Context, ref string, opts ...containerdclient.RemoteOpt) (containerdclient.Image, error)
	NewContainer(ctx context.Context, id string, opts ...containerdclient.NewContainerOpts) (containerdclient.Container, error)
	Close() error
}

// ContainerdRuntime implements runtime.Runtime using containerd.
type ContainerdRuntime struct {
	client containerdClient
	cni    gocni.CNI
}

// New creates a ContainerdRuntime connected to the local containerd socket.
func New(_ context.Context) (*ContainerdRuntime, error) {
	c, err := containerdclient.New(containerdSocket, containerdclient.WithDefaultNamespace(itestNamespace))
	if err != nil {
		return nil, fmt.Errorf("containerd client: %w", err)
	}

	cniNet, err := gocni.New(
		gocni.WithConfListBytes(bridgeCNIConfig),
		gocni.WithLoNetwork,
	)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("cni init: %w", err)
	}

	return &ContainerdRuntime{client: c, cni: cniNet}, nil
}

// Close releases the containerd client.
func (r *ContainerdRuntime) Close() error {
	return r.client.Close()
}

// pullImage returns the local image if it exists, otherwise pulls from registry.
func (r *ContainerdRuntime) pullImage(ctx context.Context, ref string) (containerdclient.Image, error) {
	image, err := r.client.GetImage(ctx, ref)
	if err == nil {
		return image, nil
	}
	// Not found locally — pull from registry.
	image, err = r.client.Pull(ctx, ref, containerdclient.WithPullUnpack)
	if err != nil {
		return nil, fmt.Errorf("pull %s: %w", ref, err)
	}
	return image, nil
}

// Run pulls the image, creates a container and task, sets up CNI networking,
// and starts the task.
func (r *ContainerdRuntime) Run(ctx context.Context, opts runtime.RunOptions) (runtime.Container, error) {
	ctx = namespaces.WithNamespace(ctx, itestNamespace)

	image, err := r.pullImage(ctx, opts.Image)
	if err != nil {
		return nil, err
	}

	containerID := generateID()

	specOpts, err := r.buildSpecOpts(image, opts)
	if err != nil {
		return nil, fmt.Errorf("buildSpecOpts: %w", err)
	}
	container, err := r.client.NewContainer(ctx,
		containerID,
		containerdclient.WithImage(image),
		containerdclient.WithNewSnapshot(containerID, image),
		containerdclient.WithNewSpec(specOpts...),
		containerdclient.WithContainerLabels(opts.Labels),
	)
	if err != nil {
		return nil, fmt.Errorf("NewContainer: %w", err)
	}

	ioCreator := r.buildIOCreator(opts.LogLine)
	task, err := container.NewTask(ctx, ioCreator)
	if err != nil {
		_ = container.Delete(ctx, containerdclient.WithSnapshotCleanup)
		return nil, fmt.Errorf("NewTask: %w", err)
	}

	portMappings := r.buildPortMappings(opts)
	nsPath := fmt.Sprintf("/proc/%d/ns/net", task.Pid())
	if _, cniErr := r.cni.Setup(ctx, containerID, nsPath, gocni.WithCapabilityPortMap(portMappings)); cniErr != nil {
		_ = task.Kill(ctx, syscall.SIGKILL)
		_, _ = task.Delete(ctx)
		_ = container.Delete(ctx, containerdclient.WithSnapshotCleanup)
		return nil, fmt.Errorf("cni setup: %w", cniErr)
	}

	if exitCh, err := task.Wait(ctx); err != nil {
		_ = r.cni.Remove(ctx, containerID, nsPath)
		_ = task.Kill(ctx, syscall.SIGKILL)
		_, _ = task.Delete(ctx)
		_ = container.Delete(ctx, containerdclient.WithSnapshotCleanup)
		return nil, fmt.Errorf("task wait channel: %w", err)
	} else if err := task.Start(ctx); err != nil {
		_ = r.cni.Remove(ctx, containerID, nsPath)
		_ = task.Kill(ctx, syscall.SIGKILL)
		_, _ = task.Delete(ctx)
		_ = container.Delete(ctx, containerdclient.WithSnapshotCleanup)
		return nil, fmt.Errorf("task start: %w", err)
	} else {
		return &containerdContainer{
			id:        containerID,
			container: container,
			task:      task,
			cni:       r.cni,
			nsPath:    nsPath,
			exitCh:    exitCh,
		}, nil
	}
}

func (r *ContainerdRuntime) buildSpecOpts(image containerdclient.Image, opts runtime.RunOptions) ([]oci.SpecOpts, error) {
	specOpts := []oci.SpecOpts{oci.WithImageConfig(image)}

	if len(opts.Env) > 0 {
		var envSlice []string
		for k, v := range opts.Env {
			envSlice = append(envSlice, k+"="+v)
		}
		specOpts = append(specOpts, oci.WithEnv(envSlice))
	}

	if len(opts.Cmd) > 0 {
		specOpts = append(specOpts, oci.WithProcessArgs(opts.Cmd...))
	}

	var specMounts []specs.Mount
	for _, m := range opts.Mounts {
		if m.Type == runtime.MountTypeVolume {
			if strings.Contains(m.Source, "/") || strings.Contains(m.Source, "..") {
				return nil, fmt.Errorf("invalid volume name %q: must not contain path separators", m.Source)
			}
			hostPath := filepath.Join(volumeBaseDir, m.Source)
			if mkErr := os.MkdirAll(hostPath, 0o755); mkErr != nil {
				return nil, fmt.Errorf("create volume dir %s: %w", hostPath, mkErr)
			}
			specMounts = append(specMounts, specs.Mount{
				Type:        "bind",
				Source:      hostPath,
				Destination: m.Target,
				Options:     []string{"rbind", "rw"},
			})
		} else if m.Type == runtime.MountTypeBind {
			specMounts = append(specMounts, specs.Mount{
				Type:        "bind",
				Source:      m.Source,
				Destination: m.Target,
				Options:     []string{"rbind", "rw"},
			})
		}
	}
	if len(specMounts) > 0 {
		specOpts = append(specOpts, oci.WithMounts(specMounts))
	}

	return specOpts, nil
}

func (r *ContainerdRuntime) buildIOCreator(logLine func(stream, line string)) cio.Creator {
	if logLine == nil {
		return cio.NullIO
	}
	return cio.NewCreator(cio.WithStreams(
		nil,
		lineWriter("stdout", logLine),
		lineWriter("stderr", logLine),
	))
}

func (r *ContainerdRuntime) buildPortMappings(opts runtime.RunOptions) []gocni.PortMapping {
	var portMappings []gocni.PortMapping
	for port, bindings := range opts.PortBindings {
		portStr := string(port)
		parts := strings.SplitN(portStr, "/", 2)
		proto := "tcp"
		if len(parts) == 2 {
			proto = parts[1]
		}
		containerPort, err := strconv.ParseInt(parts[0], 10, 32)
		if err != nil || containerPort < 1 || containerPort > 65535 {
			continue
		}
		for _, b := range bindings {
			hostPort, err := strconv.ParseInt(b.HostPort, 10, 32)
			if err != nil || hostPort < 1 || hostPort > 65535 {
				continue
			}
			portMappings = append(portMappings, gocni.PortMapping{
				HostPort:      int32(hostPort),
				ContainerPort: int32(containerPort),
				Protocol:      proto,
				HostIP:        b.HostIP.String(),
			})
		}
	}
	return portMappings
}

// containerdContainer is a running containerd container handle.
type containerdContainer struct {
	id        string
	container containerdclient.Container
	task      containerdclient.Task
	cni       gocni.CNI
	nsPath    string
	exitCh    <-chan containerdclient.ExitStatus
}

func (c *containerdContainer) ID() string { return c.id }

// Stop tears down CNI networking, kills the task, and removes the container.
// Sends SIGTERM first, waits up to 5 seconds, then SIGKILLs if still running.
func (c *containerdContainer) Stop(ctx context.Context) error {
	ctx = namespaces.WithNamespace(ctx, itestNamespace)

	_ = c.task.Kill(ctx, syscall.SIGTERM)
	select {
	case <-c.exitCh:
		// clean exit after SIGTERM
	case <-time.After(5 * time.Second):
		_ = c.task.Kill(ctx, syscall.SIGKILL)
		select {
		case <-c.exitCh:
		case <-time.After(5 * time.Second):
			// best effort — proceed with cleanup
		}
	}

	cniErr := c.cni.Remove(ctx, c.id, c.nsPath)
	_, _ = c.task.Delete(ctx)
	_ = c.container.Delete(ctx, containerdclient.WithSnapshotCleanup)

	if cniErr != nil {
		return fmt.Errorf("cni remove: %w", cniErr)
	}
	return nil
}

// generateID returns a unique container ID using crypto/rand.
func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("itestcontainer-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return "itestcontainer-" + hex.EncodeToString(b)
}

// lineWriter returns an io.Writer that calls logLine for each line.
func lineWriter(stream string, logLine func(stream, line string)) io.Writer {
	return &lineWriterImpl{stream: stream, logLine: logLine}
}

type lineWriterImpl struct {
	stream  string
	logLine func(stream, line string)
	buf     []byte
}

func (w *lineWriterImpl) Write(p []byte) (int, error) {
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
