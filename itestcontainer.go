// itestcontainer is a runner shim invoked by a `rules_itest`'s `itest_service` as an `exe` to launch a container image.
//
// Pass the name of the container, any environment the container needs, volume
// mounts, port assignments, and labels.
//
// Volumes exist in the Docker volume space on the host, but are identified
// internally with the prefix `bazel-itest-`.  If run inside the Bazel test
// execution environment (i.e. with the environment variable `TEST_TARGET` set)
// then that string is hashed and appended to the volume name.  This allows
// each `itest_service` to run concurrently, avoiding contention and potential
// locking issues.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/jaqx0r/itestcontainer/internal/docker"
	"github.com/jaqx0r/itestcontainer/internal/runtime"
)

var (
	name        = flag.String("name", "", "`name`(`:tag`) name and optional tag of the container to launch")
	volume      = flag.String("volume", "", "`name`:`path` pairs of volumes to mount.  If `TEST_TARGET` is set in the environment, that value is hashed and appended to the volume name.  The string `bazel-itest-` is always prepended.")
	env         = flag.String("env", "", "KEY[,KEY] list of environment variable names to pass through to the container")
	ports       = flag.String("ports", "", "exposed port mappings to pass to container")
	labels      = flag.String("labels", "", "labels to set on container")
	cmd         = flag.String("cmd", "", "command to run in container (space-separated)")
	runtimeFlag = flag.String("runtime", "", "container runtime to use (docker|containerd); empty = auto-detect")
)

// Config holds all parameters needed to launch a container.
type Config struct {
	Name       string
	Ports      string
	Env        string
	Volume     string
	Labels     string
	Cmd        string
	Runtime    string
	TestTarget string
	EnvLookup  func(string) (string, bool)
}

func main() {
	flag.Parse()

	cfg := Config{
		Name:       *name,
		Ports:      *ports,
		Env:        *env,
		Volume:     *volume,
		Labels:     *labels,
		Cmd:        *cmd,
		Runtime:    *runtimeFlag,
		TestTarget: os.Getenv("TEST_TARGET"),
		EnvLookup:  os.LookupEnv,
	}

	if err := run(cfg); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(cfg Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWithContext(ctx, stop, cfg)
}

// detect returns the runtime name to use, checking containerd socket then falling back to docker.
func detect() string {
	if _, err := os.Stat("/run/containerd/containerd.sock"); err == nil {
		return "containerd"
	}
	return "docker"
}

// newRuntime constructs the appropriate runtime.Runtime for the given name.
func newRuntime(ctx context.Context, name string) (runtime.Runtime, error) {
	switch name {
	case "containerd":
		return newContainerdRuntime(ctx)
	default:
		return docker.New(ctx)
	}
}

func runWithContext(ctx context.Context, stop context.CancelFunc, cfg Config) error {
	if cfg.Name == "" {
		return fmt.Errorf("`name` must be set")
	}

	exposedPorts, portBindings, err := parsePorts(cfg.Ports)
	if err != nil {
		return fmt.Errorf("parsePorts: %w", err)
	}
	log.Println("Exposed Ports:", exposedPorts)

	environment, err := parseEnvironment(cfg.Env, cfg.EnvLookup)
	if err != nil {
		return fmt.Errorf("parseEnvironment: %w", err)
	}
	log.Println("Environment:", environment)

	suffix := volumeSuffix(cfg.TestTarget)
	mounts := parseVolumes(cfg.Volume, suffix)
	log.Println("Volume Mounts:", mounts)

	labelMap, err := parseLabels(cfg.Labels)
	if err != nil {
		return fmt.Errorf("parseLabels: %w", err)
	}
	log.Println("Labels:", labelMap)

	runtimeName := cfg.Runtime
	if runtimeName == "" {
		runtimeName = detect()
	}
	log.Println("Using runtime:", runtimeName)

	rt, err := newRuntime(ctx, runtimeName)
	if err != nil {
		return fmt.Errorf("runtime init (%s): %w", runtimeName, err)
	}
	defer rt.Close()

	opts := runtime.RunOptions{
		Image:        cfg.Name,
		ExposedPorts: exposedPorts,
		PortBindings: portBindings,
		Env:          environment,
		Mounts:       mounts,
		Labels:       labelMap,
		LogLine: func(stream, line string) {
			log.Printf("%s: %s", stream, line)
		},
	}
	if cfg.Cmd != "" {
		opts.Cmd = strings.Fields(cfg.Cmd)
	}

	c, err := rt.Run(ctx, opts)

	wg := sync.WaitGroup{}
	if c != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			log.Println("Stopping", c.ID())
			stopCtx := context.Background()
			if stopErr := c.Stop(stopCtx); stopErr != nil {
				log.Printf("failed to stop container %s: %v", c.ID(), stopErr)
			}
		}()
	}

	if err != nil {
		stop()
		wg.Wait()
		return fmt.Errorf("runtime.Run(%v): %w", cfg.Name, err)
	}

	log.Println("Started", cfg.Name)
	log.Println("Waiting, press Ctrl-C to shutdown")
	<-ctx.Done()
	stop()
	wg.Wait()
	log.Println("itestcontainer done")
	return nil
}
