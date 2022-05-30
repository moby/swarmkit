# Root directory of the project (absolute path).
ROOTDIR=$(dir $(abspath $(lastword $(MAKEFILE_LIST))))

PROJECT_ROOT=github.com/shezhua/swarmkit

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
