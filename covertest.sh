#!/usr/bin/env bash

# This script expects a list of packages to run tests for, and generates a coverage output for each of those packages

export COVERPKG=$(go list ./... | grep -v /vendor/ | tr '\r\n' ,)

go_test () {
    (set -o pipefail; go test -race -tags "${DOCKER_BUILDTAGS}" -test.short -coverprofile=../../../${1}/coverage.txt -covermode=atomic -coverpkg "${COVERPKG}" ${1} 2>&1 | grep --line-buffered -v "warning: no packages being tested depend on" | sed -e 's;\(of statements in\) github.*;\1 <all subpackages>;g')
}

export -f go_test
echo ${@} | xargs -n1 -P4 bash -c 'go_test "$@"' _

