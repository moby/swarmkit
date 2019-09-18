# protobuild

[![Build Status](https://travis-ci.org/stevvooe/protobuild.svg?branch=master)](https://travis-ci.org/stevvooe/protobuild)

Build protobufs in Go, easily.

`protobuild` works by scanning the go package in a project and emitting correct
`protoc` commands, configured with the plugins, packages and details of your
choice.

It should work with both the default `golang/protobuf` and the `gogo`
toolchain. If it doesn't, we should figure out how to get there.

The main benefit is that it makes it much easier to consume external types from
vendored projects. By integrating the protoc include paths with Go's vendoring
and GOPATH, builds are much easier to keep consistent across a project.

This comes from experience with generating protobufs with `go generate` in
swarmkit and the tool used with containerd. It should replace both.

## Status

Very early stages.

## Installation

To ensure easy use with builds, we'll try to support `go get`. Install with the
following command:

```
go get -u github.com/stevvooe/protobuild
```

## Usage

Protobuild works by providing a list of Go packages in which to build the
protobufs. To get started with a project, you must do the following:

1. Create a `Protobuild.toml` file in the root of your Go project. Use the
   [example](Protobuild.toml) as a starting point.

2. Make sure that the packages where you want your protobuf files have a Go
   file in place. Usually, adding a `doc.go` file is sufficient. A package for
   protobuf should look like this:

   ```
   foo.proto
   doc.go
   ```

   Where the contents of `doc.go` will have a package declaration in it. See
   the [example](examples/foo/doc.go) for details. Make sure the package name
   corresponds to what is in the `go_package` option.

3. Run the `protobuild` command:
    ```
    go list ./... | grep -v vendor | xargs protobuild
    ```

TODO(stevvooe): Make this better.

## Contributing

Contributions are welcome.

Please ensure that commits are signed off.

For more complex PRs or design changes, please submit an issue for discussion
first.
