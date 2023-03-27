include platform.mk
.PHONY: all help test hello clean

WORKDIR=$(shell pwd)
HELLO_SRC=${WORKDIR}/example/service
HELLO_BINARY=${WORKDIR}/bin/$(PLAT)/service
CLUSTER_SRC=${WORKDIR}/example/cluster
CLUSTER_BINARY=${WORKDIR}/bin/$(PLAT)/cluster
all: clean hello cluster test

hello:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} &&\
	cd ${HELLO_SRC} && go build -o ${HELLO_BINARY}

cluster:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} &&\
	cd ${CLUSTER_SRC} && go build -o ${CLUSTER_BINARY}

test:
ifeq ($(OS),Windows_NT)

else ifeq ($(shell uname),Darwin)
	${HELLO_BINARY}
	${CLUSTER_BINARY}
else
	${HELLO_BINARY}
	${CLUSTER_BINARY}
endif

gotool:
	go fmt ${HELLO_SRC}
	go vet ${HELLO_SRC}

clean:
	@if [ -f ${HELLO_BINARY} ] ; then rm ${HELLO_BINARY} ; fi