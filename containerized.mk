# .PHONY: clean all AUTHORS fmt vet lint build binaries test integration setup generate protos checkprotos coverage ci check help install uninstall dep-validate image
.PHONY: image

IMAGE_NAME=docker/swarmkit
DOCKER_IMAGE_DIR=/go/src/${PROJECT_ROOT}

# don't bother writing every single make target. just pass the call through to
# docker and make
.DEFAULT: image
	@echo "running target $@ inside a container"
	docker run -it -v ${ROOTDIR}:${DOCKER_IMAGE_DIR} ${IMAGE_NAME} make $@

image:
	docker build -t ${IMAGE_NAME} .
