# Root directory of the project (absolute path).
ROOTDIR=$(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Base path used to install.
DESTDIR=/usr/local

# Used to populate version variable in main package.
VERSION=$(shell git describe --match 'v[0-9]*' --dirty='.m' --always)

# Project packages.
PACKAGES=$(shell go list ./... | grep -v /vendor/)

# Project binaries.
COMMANDS=swarmd swarmctl swarm-bench protoc-gen-gogoswarm
BINARIES=$(addprefix bin/,$(COMMANDS))

GO_LDFLAGS=-ldflags "-X `go list ./version`.Version=$(VERSION)"

.PHONY: clean all AUTHORS fmt vet lint build binaries test setup generate checkprotos coverage ci check help install uninstall
.DEFAULT: default

all: check build binaries test ## run fmt, vet, lint, build the binaries and run the tests

check: fmt vet lint ## run fmt, vet, lint

ci: check build binaries checkprotos coverage ## to be used by the CI

AUTHORS: .mailmap .git/HEAD
	git log --format='%aN <%aE>' | sort -fu > $@

# This only needs to be generated by hand when cutting full releases.
version/version.go:
	./version/version.sh > $@

setup: ## install dependencies
	@echo "🐳 $@"
	# TODO(stevvooe): Install these from the vendor directory
	@go get -u github.com/golang/lint/golint
	#@go get -u github.com/kisielk/errcheck
	@go get -u github.com/golang/mock/mockgen

generate: bin/protoc-gen-gogoswarm ## generate protobuf
	@echo "🐳 $@"
	@PATH=${ROOTDIR}/bin:${PATH} go generate -x ${PACKAGES}

checkprotos: generate ## check if protobufs needs to be generated again
	@echo "🐳 $@"
	@test -z "$$(git status --short | grep ".pb.go" | tee /dev/stderr)" || \
		((git diff | cat) && \
		(echo "👹 please run 'make generate' when making changes to proto files" && false))

# Depends on binaries because vet will silently fail if it can't load compiled
# imports
vet: binaries ## run go vet
	@echo "🐳 $@"
	@test -z "$$(go vet ${PACKAGES} 2>&1 | grep -v 'constant [0-9]* not a string in call to Errorf' | egrep -v '(timestamp_test.go|duration_test.go|exit status 1)' | tee /dev/stderr)"

fmt: ## run go fmt
	@echo "🐳 $@"
	@test -z "$$(gofmt -s -l . | grep -v vendor/ | grep -v ".pb.go$$" | tee /dev/stderr)" || \
		(echo "👹 please format Go code with 'gofmt -s -w'" && false)
	@test -z "$$(find . -path ./vendor -prune -o ! -name timestamp.proto ! -name duration.proto -name '*.proto' -type f -exec grep -Hn -e "^ " {} \; | tee /dev/stderr)" || \
		(echo "👹 please indent proto files with tabs only" && false)
	@test -z "$$(find . -path ./vendor -prune -o -name '*.proto' -type f -exec grep -Hn "id = " {} \; | grep -v gogoproto.customname | tee /dev/stderr)" || \
		(echo "👹 id fields in proto files must have a gogoproto.customname set" && false)
	@test -z "$$(find . -path ./vendor -prune -o -name '*.proto' -type f -exec grep -Hn "Meta meta = " {} \; | grep -v '(gogoproto.nullable) = false' | tee /dev/stderr)" || \
		(echo "👹 meta fields in proto files must have option (gogoproto.nullable) = false" && false)

lint: ## run go lint
	@echo "🐳 $@"
	@test -z "$$(golint ./... | grep -v vendor/ | grep -v ".pb.go:" | grep -v ".mock.go" | tee /dev/stderr)"

#errcheck: ## run go errcheck
#	@echo "🐳 $@"
#	@test -z "$$(errcheck ./... | grep -v vendor/ | grep -v ".pb.go:" | grep -v ".mock.go" | tee /dev/stderr)"

build: ## build the go packages
	@echo "🐳 $@"
	@go build -i -tags "${DOCKER_BUILDTAGS}" -v ${GO_LDFLAGS} ${GO_GCFLAGS} ${PACKAGES}

## run tests - first filter out packages that don't have tests because `go test` compiles and runs a test binary
# for each package, even if it has no tests (in order to make sure it compiles), which takes extra time. This skips
# the extra compilation check for untested packages, though, so this runs the build make target too.
test: build
	@echo "🐳 $@"
	@go test -parallel 8 -race -tags "${DOCKER_BUILDTAGS}" $(shell go list -f '{{ if .TestGoFiles }}{{ .ImportPath }}{{ end }}' ${PACKAGES})

FORCE:

# Build a binary from a cmd.
bin/%: cmd/% FORCE
	@test $$(go list) = "github.com/docker/swarmkit" || \
		(echo "👹 Please correctly set up your Go build environment. This project must be located at <GOPATH>/src/github.com/docker/swarmkit" && false)
	@echo "🐳 $@"
	@go build -i -tags "${DOCKER_BUILDTAGS}" -o $@ ${GO_LDFLAGS}  ${GO_GCFLAGS} ./$<

binaries: $(BINARIES) ## build binaries
	@echo "🐳 $@"

clean: ## clean up binaries
	@echo "🐳 $@"
	@rm -f $(BINARIES)
	@find . -name coverage.txt -delete

install: $(BINARIES) ## install binaries
	@echo "🐳 $@"
	@mkdir -p $(DESTDIR)/bin
	@install $(BINARIES) $(DESTDIR)/bin

uninstall:
	@echo "🐳 $@"
	@rm -f $(addprefix $(DESTDIR)/bin/,$(notdir $(BINARIES)))

## generate coverprofiles from the tests - first filter out packages that don't have tests because `go test` compiles
# and runs a test binary for each package, even if it has no tests (in order to make sure it compiles),
# which takes extra time. Particularly here, where we're compiling with coverage instrumentation.
# Filtering out no-test packages skips the extra compilation check, though, so this runs the build make target too.
coverage: build
	@echo "🐳 $@"
	@( for pkg in $(shell go list -f '{{ if .TestGoFiles }}{{ .ImportPath }}{{ end }}' ${PACKAGES}); do \
		go test -i -race -tags "${DOCKER_BUILDTAGS}" -test.short -coverprofile="../../../$$pkg/coverage.txt" -covermode=atomic $$pkg || exit; \
		go test -race -tags "${DOCKER_BUILDTAGS}" -test.short -coverprofile="../../../$$pkg/coverage.txt" -covermode=atomic $$pkg || exit; \
	done )

help: ## this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort
