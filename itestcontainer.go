// itestcontainer is a runner shim invoked by a `rules_itest`'s `itest_service` as an `exe` to launch a container image with `testcontainers`.
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
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
)

var (
	name   = flag.String("name", "", "`name`(`:tag`) name and optional tag of the container to launch")
	volume = flag.String("volume", "", "`name`:`path` pairs of volumes to mount.  If `TEST_TARGET` is set in the environment, that value is hashed and appended to the volume name.  The string `bazel-itest-` is always prepended.")
	env    = flag.String("env", "", "KEY[,KEY] list of environment variable names to pass through to the container")
	ports  = flag.String("ports", "", "exposed port mappings to pass to container")
	labels = flag.String("labels", "", "labels to set on container")
	cmd    = flag.String("cmd", "", "command to run in container (space-separated)")
)

// Config holds all parameters needed to launch a container.
type Config struct {
	Name       string
	Ports      string
	Env        string
	Volume     string
	Labels     string
	Cmd        string
	TestTarget string
	EnvLookup  func(string) (string, bool)
}

type logConsumer struct{}

func (logConsumer) Accept(l testcontainers.Log) {
	log.Printf("%s: %s", l.LogType, l.Content)
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

	lc := logConsumer{}

	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts(exposedPorts...),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.PortBindings = portBindings
		}),
		testcontainers.WithLogConsumers(lc),
		testcontainers.WithEnv(environment),
		testcontainers.WithMounts(mounts...),
		testcontainers.WithLabels(labelMap),
	}
	if cfg.Cmd != "" {
		opts = append(opts, testcontainers.WithCmd(strings.Fields(cfg.Cmd)...))
	}

	c, err := testcontainers.Run(ctx, cfg.Name, opts...)

	wg := sync.WaitGroup{}
	if c != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			containerName := c.GetContainerID()
			inspectCtx, inspectCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer inspectCancel()
			if n, inspectErr := c.Inspect(inspectCtx); inspectErr == nil {
				containerName = n.Name
			}
			log.Println("Stopping", containerName)
			if termErr := testcontainers.TerminateContainer(c); termErr != nil {
				log.Printf("failed to terminate container %s: %v", containerName, termErr)
			}
		}()
	}

	if err != nil {
		stop()
		wg.Wait()
		return fmt.Errorf("testcontainers.Run(%v): %w", cfg.Name, err)
	}

	log.Println("Started", cfg.Name)
	log.Println("Waiting, press Ctrl-C to shutdown")
	<-ctx.Done()
	stop()
	wg.Wait()
	log.Println("itestcontainer done")
	return nil
}
