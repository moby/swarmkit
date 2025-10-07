IMAGE_NAME=docker/swarmkit
GOPATH=/go
DOCKER_IMAGE_DIR=${GOPATH}/src/github.com/docker/swarmkit
BUILDX_CMD ?= docker buildx

DOCKER_SWARMKIT_DELVE_PORT ?= 2345

# don't bother writing every single make target. just pass the call through to
# docker and make
# we prefer `%:` to `.DEFAULT` as the latter doesn't run phony deps
# (see https://www.gnu.org/software/make/manual/html_node/Special-Targets.html)
%::
	@ echo "Running target $@ inside a container"
	@ DOCKER_SWARMKIT_DOCKER_RUN_CMD="make $*" $(MAKE) run

shell:
	@ DOCKER_SWARMKIT_DOCKER_RUN_CMD='bash' DOCKER_SWARMKIT_DOCKER_RUN_FLAGS='-i' $(MAKE) run

.PHONY: image
image:
	docker build -t ${IMAGE_NAME} .

# internal target, only builds the image if it doesn't exist
.PHONY: ensure_image_exists
ensure_image_exists:
	@ if [ ! $$(docker images -q ${IMAGE_NAME}) ]; then $(MAKE) image; fi

# internal target, starts the sync if needed
# uses https://github.com/EugenMayer/docker-sync/blob/47363ee31b71810a60b05822b9c4bd2176951ce8/tasks/sync/sync.thor#L193-L196
# which is not great, but that's all they expose so far to do this...
# checks if the daemon pid in the .docker-sync directory maps to a running
# process owned by the current user, and otherwise assumes the sync is not
# running, and starts it
.PHONY: ensure_sync_started
ensure_sync_started:
	@ kill -0 $$(cat .docker-sync/daemon.pid) 2&> /dev/null || docker-sync start

# internal target, actually runs a command inside a container
# we don't use the `-i` flag for `docker run` by default as that makes it a pain
# to kill running containers (can't kill with ctrl-c)
.PHONY: run
run: ensure_image_exists
	@ [ "$$DOCKER_SWARMKIT_DOCKER_RUN_CMD" ] || exit 1
	@ DOCKER_RUN_COMMAND="docker run -t -v swarmkit-cache:${GOPATH}" \
		&& if [ "$$DOCKER_SWARMKIT_USE_DOCKER_SYNC" ]; then \
			$(MAKE) ensure_sync_started && DOCKER_RUN_COMMAND+=" -v swarmkit-sync:${DOCKER_IMAGE_DIR}"; \
		elif [ ! "$$DOCKER_SWARMKIT_NOMOUNT" ] \
			DOCKER_RUN_COMMAND+=" -v ${ROOTDIR}:${DOCKER_IMAGE_DIR}"; \
		fi \
		&& if [ "$$DOCKER_SWARMKIT_USE_DELVE" ]; then \
			DOCKER_RUN_COMMAND="DOCKER_SWARMKIT_DELVE_PORT=${DOCKER_SWARMKIT_DELVE_PORT} $$DOCKER_RUN_COMMAND" ; \
			DOCKER_RUN_COMMAND+=" -p ${DOCKER_SWARMKIT_DELVE_PORT}:${DOCKER_SWARMKIT_DELVE_PORT} -e DOCKER_SWARMKIT_DELVE_PORT"; \
			`# see https://github.com/derekparker/delve/issues/515#issuecomment-214911481'` ; \
			DOCKER_RUN_COMMAND+=" --security-opt=seccomp:unconfined"; \
		fi \
		&& DOCKER_RUN_COMMAND+=" $$DOCKER_SWARMKIT_DOCKER_RUN_FLAGS $$DOCKER_SWARMKIT_EXTRA_RUN_FLAGS ${IMAGE_NAME} $$DOCKER_SWARMKIT_DOCKER_RUN_CMD" \
		&& echo $$DOCKER_RUN_COMMAND \
		&& eval $$DOCKER_RUN_COMMAND

# use buildx/bake for the following targets
.PHONY: fmt-proto
fmt-proto:
	$(BUILDX_CMD) bake fmt-proto

.PHONY: lint
lint:
	$(BUILDX_CMD) bake lint

.PHONY: vendor-validate
vendor-validate:
	$(BUILDX_CMD) bake vendor-validate

.PHONY: generate-validate
generate-validate:
	$(BUILDX_CMD) bake generate-validate
