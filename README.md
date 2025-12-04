# itestcontainer

`itestcontainer` is a runner shim invoked by [`rules_itest`](https://github.com/dzbarsky/rules_itest)'s [`itest_service`](https://github.com/dzbarsky/rules_itest/blob/master/docs/itest.md#itest_service) as an `exe` to launch a container image as the system under test.

Example:

```skylark
load("@rules_img//img:image.bzl", "image_manifest")
load("@rules_img//img:load.bzl", "image_load")
load("@rules_itest//:itest.bzl", "itest_service", "itest_task")

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
        # Ports to expose from the container
        "--ports=5432:$${@@:sut:5432}"
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
        "5432",
    ],
    deps = [":load_pg_image_task"],
)

```
