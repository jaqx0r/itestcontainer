# itestcontainer

`itestcontainer` is a runner shim invoked by [`rules_itest`](https://github.com/dzbarsky/rules_itest)'s [`itest_service`](https://github.com/dzbarsky/rules_itest/blob/master/docs/itest.md#itest_service) as an `exe` to launch a container image as the system under test.

## Runtime Backends

`itestcontainer` supports two container runtimes:

| Runtime      | Backend           | Networking               |
|--------------|-------------------|--------------------------|
| `docker`     | moby/moby client  | Docker bridge networking |
| `containerd` | containerd/v2     | CNI (bridge + portmap)   |

By default, `itestcontainer` expects images to be pre-loaded into the container runtime.

By default, `itestcontainer` auto-detects the runtime: it probes the containerd socket first (`/run/containerd/containerd.sock`), then falls back to Docker (`DOCKER_HOST` or `/var/run/docker.sock`). Override with `--runtime=docker` or `--runtime=containerd`.

### Image Pulling
`itestcontainer` assumes images are available in the local runtime cache. If an image is missing, it will fail to start.

You can enable automatic image pulling by using the `PullImages` flag in `RunOptions` (if using as a Go library) or via corresponding command-line flags. This functionality should be used sparingly or only for local development, as it introduces external dependencies during test execution.

### containerd Prerequisites

- containerd running with a socket accessible at `/run/containerd/containerd.sock`
- CNI plugin binaries installed: `bridge`, `portmap`, `loopback` (typically under `/opt/cni/bin`)

## Flags

| Flag        | Description                                                               |
|-------------|---------------------------------------------------------------------------|
| `--name`    | Container image to start (required)                                       |
| `--runtime` | Runtime backend: `docker` or `containerd` (default: auto-detect)         |
| `--ports`   | Port mappings, comma-separated `<host>:<container>` (e.g. `8080:80/tcp`) |
| `--pull-images` | Enable automatic image pulling if not found locally (default: false). |
| `--env`     | Environment variables to pass through, comma-separated                    |
| `--volume`  | Volume mounts, comma-separated `<source>:<target>`                        |
| `--labels`  | Container labels, comma-separated `<key>=<value>`                         |

By default, `itestcontainer` assumes images are already loaded into the container runtime. The `--pull-images` flag should only be used if automatic pulling is required for local development workflows.

Example:

```skylark
load("@rules_img//img:image.bzl", "image_manifest")
load("@rules_img//img:load.bzl", "image_load")
load("@rules_itest//:itest.bzl", "itest_service", "itest_task", "named_port")

platform(
    name = "host_docker_platform",
    constraint_values = ["@platforms//os:linux"],  # linux OS inside docker host
    parents = ["@platforms//host"],  # use host CPU
)

# Construct a Postgresql for the container host
image_manifest(
    name = "pg_image",
    base = "@postgresql",
    platform = ":host_docker_platform",
)

# An executable target to load the image into the container host
image_load(
    name = "load_pg_image",
    image = ":pg_image",
    tag = "pg_image:latest",
)

# A task that invokes the previous executable target
itest_task(
    name = "load_pg_image_task",
    testonly = True,
    exe = ":load_pg_image",
)

# The service under test, which depends on the load task to load the image, and then invokes `itestcontainer` to run the image with the provided options.
itest_service(
    name = "sut",
    testonly = True,
    args = [
        # Name of the image to start
        "--name=pg_image:latest",
        # Environment variables to pass through to the container
        "--env=POSTGRES_USER,POSTGRES_PASSWORD,POSTGRES_DB",
        # Ports to expose from the container, using `rules_test`'s helper macro
        "--ports=" + named_port("//:sut", "db") + ":5432",
        # --labels
        # --volume
    ],
    env = {
        "POSTGRES_USER": "postgres",
        "POSTGRES_PASSWORD": "postgres",
        "POSTGRES_DB": "postgres",
    },
    exe = "@com_github_jaqx0r_itestcontainer//:itestcontainer",
    named_ports = [
        "db",
    ],
    deps = [":load_pg_image_task"],
)

```

Use the Go Tools Pattern to include `itestcontainer` into your dependencies.

See [`test/tools.go`](test/tools.go) and the rest of the test directory for a full example.
