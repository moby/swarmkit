## Build the development environment

To build Swarmkit, you must set up a Go development environment.
[How to Write Go Code](https://golang.org/doc/code.html) contains full instructions.
When setup correctly, you should have a GOROOT and GOPATH set in the environment.

After you set up the Go development environment, use `go get` to check out
`swarmkit`:

    go get -d github.com/moby/swarmkit/v2@latest

This command installs the source repository into the `GOPATH`.

It is not mandatory to use `go get` to checkout the SwarmKit project. However,
for these instructions to work, you need to check out the project to the
correct subdirectory of the `GOPATH`: `$GOPATH/src/github.com/moby/swarmkit`.

### Repeatable Builds

For the full development experience, one should `cd` into
`$GOPATH/src/github.com/moby/swarmkit`. From there, the regular `go`
commands, such as `go test`, should work per package (please see
[Developing](#developing) if they don't work).

Docker provides a `Makefile` as a convenience to support repeatable builds.
`make setup` installs tools onto the `GOPATH` for use with developing
`SwarmKit`:

    make setup

Once these commands are available in the `GOPATH`, run `make` to get a full
build:

    $ make
    🐳 fmt
    🐳 bin/swarmd
    🐳 bin/swarmctl
    🐳 bin/swarm-bench
    🐳 bin/protoc-gen-gogoswarm
    🐳 binaries
    🐳 vet
    🐳 lint
    🐳 build
    github.com/moby/swarmkit
    github.com/moby/swarmkit/vendor/github.com/davecgh/go-spew/spew
    github.com/moby/swarmkit/vendor/github.com/pmezard/go-difflib/difflib
    github.com/moby/swarmkit/cmd/protoc-gen-gogoswarm
    github.com/moby/swarmkit/cmd/swarm-bench
    github.com/moby/swarmkit/cmd/swarmctl
    github.com/moby/swarmkit/vendor/github.com/stretchr/testify/assert
    github.com/moby/swarmkit/ca/testutils
    github.com/moby/swarmkit/cmd/swarmd
    github.com/moby/swarmkit/vendor/code.cloudfoundry.org/clock/fakeclock
    github.com/moby/swarmkit/vendor/github.com/stretchr/testify/require
    github.com/moby/swarmkit/manager/state/raft/testutils
    github.com/moby/swarmkit/manager/testcluster
    github.com/moby/swarmkit/protobuf/plugin/deepcopy/test
    github.com/moby/swarmkit/protobuf/plugin/raftproxy/test
    🐳 test
    ?       github.com/moby/swarmkit      [no test files]
    ?       github.com/moby/swarmkit      [no test files]
    ok      github.com/moby/swarmkit/agent        2.264s
    ok      github.com/moby/swarmkit/agent/exec   1.055s
    ok      github.com/moby/swarmkit/agent/exec/container 1.094s
    ?       github.com/moby/swarmkit/api  [no test files]
    ?       github.com/moby/swarmkit/api/duration [no test files]
    ?       github.com/moby/swarmkit/api/timestamp        [no test files]
    ok      github.com/moby/swarmkit/ca   15.634s
    ...
    ok      github.com/moby/swarmkit/protobuf/plugin/raftproxy/test       1.084s
    ok      github.com/moby/swarmkit/protobuf/ptypes      1.025s
    ?       github.com/moby/swarmkit/version      [no test files]

The above provides a repeatable build using the contents of the vendored
`./vendor` directory. This includes formatting, vetting, linting, building,
and testing. The binaries created will be available in `./bin`.

Several `make` targets are provided for common tasks. Please see the `Makefile`
for details.

### Update vendored dependencies

To update dependency you need just change `go.mod` file and run:
```
make go-mod-vendor
```

### Regenerating protobuf bindings

This requires that you have [Protobuf 3.x or
higher](https://developers.google.com/protocol-buffers/docs/downloads). Once
that is installed the bindings can be regenerated with:

```
make setup
make generate
```

NB: As of version 3.0.0-7 the Debian `protobuf-compiler` package lacks
a dependency on `libprotobuf-dev` which contains some standard proto
definitions, be sure to install both packages. This is [Debian bug
#842158](https://bugs.debian.org/842158).

### Build in a container instead of your local environment

You can also choose to use a container to build SwarmKit and run tests. Simply
set the `DOCKER_SWARMKIT_USE_CONTAINER` environment variable to any value,
export it, then run `make` targets as you would have done within your local
environment.

Additionally, if your OS is not Linux, you might want to set and export the
`DOCKER_SWARMKIT_USE_DOCKER_SYNC` environment variable, which will make use of
[docker-sync](https://github.com/EugenMayer/docker-sync) to sync the code to
the container, instead of native mounted volumes.
