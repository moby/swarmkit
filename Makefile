# Root directory of the project (absolute path).
ROOTDIR=$(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Base path used to install.
DESTDIR=/usr/local

# Used to populate version variable in main package.
VERSION=$(shell git describe --match 'v[0-9]*' --dirty='.m' --always)

PROJECT_ROOT=github.com/docker/swarmkit

# Race detector is only supported on amd64.
RACE := $(shell test $$(go env GOARCH) != "amd64" || (echo "-race"))

# Project packages.
PACKAGES=$(shell go list ./... | grep -v /vendor/)
INTEGRATION_PACKAGE=${PROJECT_ROOT}/integration

# Project binaries.
COMMANDS=swarmd swarmctl swarm-bench swarm-rafttool protoc-gen-gogoswarm
BINARIES=$(addprefix bin/,$(COMMANDS))

VNDR=$(shell which vndr || echo '')

GO_LDFLAGS=-ldflags "-X `go list ./version`.Version=$(VERSION)"

SHELL := /bin/bash

# stop here. do we want to run everything inside of a container, or do we want
# to run it directly on the host? if the user has set ANY non-empty value for
# the variable DOCKER_SWARMKIT_USE_CONTAINER, then we do all of the making
# inside of a container. We will default to using no container, to avoid
# breaking anyone's workflow
ifdef DOCKER_SWARMKIT_USE_CONTAINER
include containerized.mk
else
include direct.mk
endif
