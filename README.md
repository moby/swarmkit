# Swarm: Cluster orchestration for Docker

[![GoDoc](https://godoc.org/github.com/docker/swarm-v2?status.png)](https://godoc.org/github.com/docker/swarm-v2)
[![Circle CI](https://circleci.com/gh/docker/swarm-v2.svg?style=shield&circle-token=a7bf494e28963703a59de71cf19b73ad546058a7)](https://circleci.com/gh/docker/swarm-v2)
[![codecov.io](https://codecov.io/github/docker/swarm-v2/coverage.svg?branch=master&token=LqD1dzTjsN)](https://codecov.io/github/docker/swarm-v2?branch=master)

## Build

Requirements:

- go 1.6
- A [working golang](https://golang.org/doc/code.html) environment


From the project root directory run:

```bash
$ make binaries
```

## Test

Before running tests for the first time, setup the tooling:

```bash
$ make setup
```

Then run:

```bash
$ make all
```

