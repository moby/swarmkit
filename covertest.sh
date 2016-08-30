#!/usr/bin/env bash

# This script expects a list of packages to run tests for, and generates a coverage output for each of those packages

COVERPKG=$(go list ./... | grep -v /vendor/ | tr '\r\n' ,)

for pkg in ${@}; do
    # installs dependent packages, without running tests
    go test -i -race -tags "${DOCKER_BUILDTAGS}" -test.short -coverprofile="../../../${pkg}/coverage.txt" -covermode=atomic -coverpkg "${COVERPKG}" ${pkg} || exit;
    # actually run test
    (set -o pipefail;
     go test -race -tags "${DOCKER_BUILDTAGS}" -test.short -coverprofile="../../../${pkg}/coverage.txt" -covermode=atomic -coverpkg "${COVERPKG}" ${pkg} 2>&1 | grep --line-buffered -v "warning: no packages being tested depend on" | sed -e 's;\(of statements in\) github.*;\1 <all subpackages>;g') || exit;
done
