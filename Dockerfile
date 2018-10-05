# NOTE(dperny): for some reason, alpine was giving me trouble
FROM golang:1.11.0-stretch

RUN apt-get update && apt-get install -y make git unzip

WORKDIR /go/src/github.com/docker/swarmkit/

# install the dependencies needed to run tests
# we only copy `direct.mk` to avoid busting the cache too easily
COPY direct.mk .
RUN make --file=direct.mk ci-prep

# now we can copy the rest
COPY . .

# default to just `make`. If you want to change the default command, change the
# default make command, not this command.
CMD ["make"]
