# NOTE(dperny): for some reason, alpine was giving me trouble
FROM golang:1.10.3-stretch

RUN apt-get update && apt-get install -y make git unzip

# should stay consistent with the version in .circleci/config.yml
ARG PROTOC_VERSION=3.6.1
# download and install protoc binary and .proto files
RUN curl --silent --show-error --location --output protoc.zip \
  https://github.com/google/protobuf/releases/download/v$PROTOC_VERSION/protoc-$PROTOC_VERSION-linux-x86_64.zip \
  && unzip -d /usr/local protoc.zip include/\* bin/\* \
  && rm -f protoc.zip

WORKDIR /go/src/github.com/docker/swarmkit/

# install the dependencies from `make setup`
# we only copy `direct.mk` to avoid busting the cache too easily
COPY direct.mk .
RUN make --file=direct.mk setup

# now we can copy the rest
COPY . .

# default to just `make`. If you want to change the default command, change the
# default make command, not this command.
CMD ["make"]
