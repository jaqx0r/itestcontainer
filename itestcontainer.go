// itestcontainer is a runner shim invoked by `rules_itest`'s `itest_service` as an `exe` to launch a container image, using `testcontainers`.
//
// Pass the name of the container, any environment the container needs, and any volumes to mount.
//
// Exposed ports are inferred from the ASSIGNED_PORTS environment variable. Use
// the `named_ports` parameter to `itest_service` and use the internal port to
// expose as the port name.  `itest` will assign a port on the host.
//
// Volumes exist in the Docker volume space on the host, but are identified
// with the prefix `bazel-itest-`, and if run with the text execution
// environment in Bazel (i.e. with the environment variable `TEST_TARGET` set)
// then a hash that uniquely identifies the test will be appended to the volume
// name.  This allows tests to run concurrently but avoid contention and
// potential locking issues when sharing a volume name.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/testcontainers/testcontainers-go"
)

var (
	name   = flag.String("name", "", "`name`(`:tag`) name and optional tag of the container to launch")
	volume = flag.String("volume", "", "`name`:`path` pairs of volumes to mount.  If `TEST_TARGET` is set in the environment, that value is hashed and appended to the volume name.  The string `bazel-itest-` is always prepended.")
	env    = flag.String("env", "", "KEY[,KEY] list of environment variable names to pass through to the container")
	ports  = flag.String("ports", "", "exposed port mappings to pass to container")
	labels = flag.String("labels", "", "labels to set on container")
)

type logConsumer struct {
}

func (logConsumer) Accept(l testcontainers.Log) {
	log.Printf("%s: %s", l.LogType, l.Content)
}

func main() {
	flag.Parse()

	if *name == "" {
		log.Fatal("`name` must be set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	wg := sync.WaitGroup{}

	exposedPorts := make([]string, 0)
	for portMap := range strings.SplitSeq(*ports, ",") {
		if portMap == "" {
			continue
		}
		exposedPorts = append(exposedPorts, portMap)
	}
	log.Println("Exposed Ports:", exposedPorts)

	environment := make(map[string]string, 0)
	for envVar := range strings.SplitSeq(*env, ",") {
		if envVar == "" {
			continue
		}
		value := os.Getenv(envVar)
		if value == "" {
			log.Fatalf("No environment variable found: %q", envVar)
		}
		environment[envVar] = value
	}
	log.Println("Environment:", environment)

	// Create a mount name suffix for the volume based on TEST_TARGET.
	suffix := ""
	testTarget := os.Getenv("TEST_TARGET")
	if testTarget != "" {
		hasher := sha256.New()
		hasher.Write([]byte(testTarget))
		hB := hasher.Sum(nil)
		suffix = hex.EncodeToString(hB)
	}
	mounts := make([]testcontainers.ContainerMount, 0)
	for volumeMount := range strings.SplitSeq(*volume, ",") {
		if volumeMount == "" {
			continue
		}
		parts := strings.SplitN(volumeMount, ":", 2)
		volumeName := ""
		if suffix != "" {
			volumeName = fmt.Sprintf("bazel-itest-%s-%s", parts[0], suffix)
		} else {
			volumeName = fmt.Sprintf("bazel-itest-%s", parts[0])
		}
		mounts = append(mounts,
			testcontainers.ContainerMount{
				Source: testcontainers.GenericVolumeMountSource{Name: volumeName},
				Target: testcontainers.ContainerMountTarget(parts[1]),
			})
	}
	log.Println("Volume Mounts:", mounts)

	labelMap := make(map[string]string, 0)
	for label := range strings.SplitSeq(*labels, ",") {
		if label == "" {
			continue
		}
		pair := strings.SplitN(label, "=", 2)
		labelMap[pair[0]] = pair[1]
	}
	log.Println("Labels:", labelMap)

	logConsumer := logConsumer{}

	c, err := testcontainers.Run(ctx, *name,
		testcontainers.WithExposedPorts(exposedPorts...),
		testcontainers.WithLogConsumers(logConsumer),
		testcontainers.WithEnv(environment),
		testcontainers.WithMounts(mounts...),
		testcontainers.WithLabels(labelMap),
	)
	if err != nil {
		log.Fatalf("testcontainers.Run(%v): %v", *name, err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		name := c.GetContainerID()
		n, err := c.Inspect(ctx)
		if err != nil {
			name = n.Name
		}
		<-ctx.Done()
		log.Println("Stopping ", name)
		testcontainers.TerminateContainer(c)
	}()
	log.Println("Started", *name)
	log.Println("Waiting, press Ctrl-C to shutdown")
	<-ctx.Done()
	stop()
	wg.Wait()
	log.Println("itestcontainer done")
}
