# NOTE(dperny): for some reason, alpine was giving me trouble
ARG GO_VERSION=1.17.4
ARG DEBIAN_FRONTEND=noninteractive
ARG BASE_DEBIAN_DISTRO="buster"
ARG GOLANG_IMAGE="golang:${GO_VERSION}-${BASE_DEBIAN_DISTRO}"

FROM ${GOLANG_IMAGE}

RUN apt-get update && apt-get install -y make git unzip

# should stay consistent with the version in .circleci/config.yml
ARG PROTOC_VERSION=3.11.4
# download and install protoc binary and .proto files
RUN curl --silent --show-error --location --output protoc.zip \
  https://github.com/protocolbuffers/protobuf/releases/download/v$PROTOC_VERSION/protoc-$PROTOC_VERSION-linux-x86_64.zip \
  && unzip -d /usr/local protoc.zip include/\* bin/\* \
  && rm -f protoc.zip

ENV GO111MODULE=off
WORKDIR /go/src/github.com/docker/swarmkit/

# install the dependencies from `make setup`
# we only copy `direct.mk` to avoid busting the cache too easily
COPY direct.mk .
COPY tools .
RUN make --file=direct.mk setup

# now we can copy the rest
COPY . .

# default to just `make`. If you want to change the default command, change the
# default make command, not this command.
CMD ["make"]
