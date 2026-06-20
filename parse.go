package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/jaqx0r/itestcontainer/internal/runtime"
)

// parsePorts parses "hostPort:containerPort/proto,..." into exposed ports list and port bindings map.
// Returns error on malformed input.
func parsePorts(raw string) ([]string, map[runtime.Port][]runtime.PortBinding, error) {
	exposedPorts := make([]string, 0)
	portBindings := make(map[runtime.Port][]runtime.PortBinding)

	for portMap := range strings.SplitSeq(raw, ",") {
		if portMap == "" {
			continue
		}
		portPair := strings.Split(portMap, ":")
		if len(portPair) != 2 {
			continue
		}
		hostPortNum, err := strconv.Atoi(portPair[0])
		if err != nil || hostPortNum < 1 || hostPortNum > 65535 {
			return nil, nil, fmt.Errorf("invalid host port %q", portPair[0])
		}
		// Default protocol to tcp if not specified (e.g. "80" → "80/tcp").
		containerPortRaw := portPair[1]
		if !strings.Contains(containerPortRaw, "/") {
			containerPortRaw = containerPortRaw + "/tcp"
		}
		_, portErr := parsePort(containerPortRaw)
		if portErr != nil {
			return nil, nil, fmt.Errorf("invalid port %q: %w", containerPortRaw, portErr)
		}
		port := runtime.Port(containerPortRaw)
		exposedPorts = append(exposedPorts, containerPortRaw)
		portBindings[port] = []runtime.PortBinding{
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
func parseVolumes(raw string, suffix string) []runtime.Mount {
	mounts := make([]runtime.Mount, 0)

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
		mounts = append(mounts, runtime.Mount{
			Type:   runtime.MountTypeVolume,
			Source: volumeName,
			Target: parts[1],
		})
	}

	return mounts
}

// parsePort validates a port spec like "80/tcp". Returns the Port or an error.
func parsePort(raw string) (runtime.Port, error) {
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid port format %q: expected <port>/<proto>", raw)
	}
	port, err := strconv.Atoi(parts[0])
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid port number %q", parts[0])
	}
	proto := strings.ToLower(parts[1])
	switch proto {
	case "tcp", "udp", "sctp":
	default:
		return "", fmt.Errorf("invalid protocol %q", proto)
	}
	return runtime.Port(raw), nil
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
