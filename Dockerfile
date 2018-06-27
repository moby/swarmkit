# NOTE(dperny): for some reason, alpine was giving me trouble
FROM golang:1.10.3-stretch

RUN apt-get update && apt-get install -y make git unzip

# make a directory to do these operations in
RUN mkdir -p protoc && cd protoc && \
  # download the pre-built protoc binary
  curl --silent --show-error --location --output protoc.zip \
  https://github.com/google/protobuf/releases/download/v3.5.0/protoc-3.5.0-linux-x86_64.zip \
  # move the binary to /bin. move the well-known types ot /usr/local/include
  && unzip protoc.zip && mv bin/protoc /bin/protoc && mv include/* /usr/local/include \
  # remove all of the files
  && rmdir bin && rmdir include && rm readme.txt \
  # remove the zip file. go back up one level and remove the now-empty
  # directory protoc
  && rm protoc.zip && cd .. && rmdir protoc

# This is sort of a workaround... basically, we want to be able to install all
# of the dependencies specified in `make setup`. But if we wait until after we
# copy code over, we'll bust the cache and reinstall these dependencies every
# time, which would be bad. This is a common problem in docker.
RUN go get -u github.com/golang/lint/golint &&\
  go get -u github.com/gordonklaus/ineffassign &&\
  go get -u github.com/client9/misspell/cmd/misspell &&\
  go get -u github.com/lk4d4/vndr &&\
  go get -u github.com/stevvooe/protobuild

WORKDIR /go/src/github.com/docker/swarmkit/
COPY . .

# default to just `make`. If you want to change the default command, change the
# default make command, not this command.
CMD ["make"]
