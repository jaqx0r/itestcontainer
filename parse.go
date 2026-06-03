package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"

	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
)

// parsePorts parses "hostPort:containerPort/proto,..." into exposed ports list and port bindings map.
// Returns error on malformed input.
func parsePorts(raw string) ([]string, network.PortMap, error) {
	exposedPorts := make([]string, 0)
	portBindings := network.PortMap{}

	for portMap := range strings.SplitSeq(raw, ",") {
		if portMap == "" {
			continue
		}
		portPair := strings.Split(portMap, ":")
		if len(portPair) != 2 {
			continue
		}
		port, portErr := network.ParsePort(portPair[1])
		if portErr != nil {
			return nil, nil, fmt.Errorf("invalid port %q: %w", portPair[1], portErr)
		}
		exposedPorts = append(exposedPorts, portPair[1])
		portBindings[port] = []network.PortBinding{
			{
				HostIP:   netip.MustParseAddr("127.0.0.1"),
				HostPort: portPair[0],
			},
		}
	}

	return exposedPorts, portBindings, nil
}

// parseEnvironment parses a comma-separated list of env var names, looking up each via lookup.
// Returns error if any var is not set.
func parseEnvironment(raw string, lookup func(string) (string, bool)) (map[string]string, error) {
	environment := make(map[string]string)

	for envVar := range strings.SplitSeq(raw, ",") {
		if envVar == "" {
			continue
		}
		value, ok := lookup(envVar)
		if !ok {
			return nil, fmt.Errorf("environment variable not set: %q", envVar)
		}
		environment[envVar] = value
	}

	return environment, nil
}

// volumeSuffix returns a hex-encoded sha256 of testTarget, or "" if testTarget is empty.
func volumeSuffix(testTarget string) string {
	if testTarget == "" {
		return ""
	}
	hasher := sha256.New()
	hasher.Write([]byte(testTarget))
	return hex.EncodeToString(hasher.Sum(nil))
}

// parseVolumes parses "name:path,..." volume mount specs, prepending "bazel-itest-" and suffix.
func parseVolumes(raw string, suffix string) []testcontainers.ContainerMount {
	mounts := make([]testcontainers.ContainerMount, 0)

	for volumeMount := range strings.SplitSeq(raw, ",") {
		if volumeMount == "" {
			continue
		}
		parts := strings.SplitN(volumeMount, ":", 2)
		if len(parts) < 2 {
			continue
		}
		var volumeName string
		if suffix != "" {
			volumeName = fmt.Sprintf("bazel-itest-%s-%s", parts[0], suffix)
		} else {
			volumeName = fmt.Sprintf("bazel-itest-%s", parts[0])
		}
		mounts = append(mounts, testcontainers.ContainerMount{
			Source: testcontainers.GenericVolumeMountSource{Name: volumeName},
			Target: testcontainers.ContainerMountTarget(parts[1]),
		})
	}

	return mounts
}

// parseLabels parses "key=val,..." label specs into a map.
// Returns error if a label has no "=" separator.
func parseLabels(raw string) (map[string]string, error) {
	labelMap := make(map[string]string)

	for label := range strings.SplitSeq(raw, ",") {
		if label == "" {
			continue
		}
		pair := strings.SplitN(label, "=", 2)
		if len(pair) < 2 {
			return nil, fmt.Errorf("malformed label %q: missing '='", label)
		}
		labelMap[pair[0]] = pair[1]
	}

	return labelMap, nil
}
