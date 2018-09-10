# NOTE(dperny): for some reason, alpine was giving me trouble
FROM golang:1.10.3-stretch

RUN apt-get update && apt-get install -y make git unzip

# should stay consistent with the version we use in Circle builds
ARG PROTOC_VERSION=3.5.0
# make a directory to do these operations in
RUN export PROTOC_TMP_DIR=protoc && mkdir -p $PROTOC_TMP_DIR && cd $PROTOC_TMP_DIR \
  # download the pre-built protoc binary
  && curl --silent --show-error --location --output protoc.zip \
    https://github.com/google/protobuf/releases/download/v$PROTOC_VERSION/protoc-$PROTOC_VERSION-linux-x86_64.zip \
  # move the binary to /bin. move the well-known types ot /usr/local/include
  && unzip protoc.zip && mv bin/protoc /bin/protoc && mv include/* /usr/local/include \
  # remove all of the installation files
  && cd .. && rm -rf $PROTOC_TMP_DIR

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
