include platform.mk
.PHONY: all help test hello clean

WORKDIR=$(shell pwd)
HELLO_SRC=${WORKDIR}/example/service
HELLO_BINARY=${WORKDIR}/bin/$(PLAT)/service

CLUSTER1_SRC=${WORKDIR}/example/cluster1
CLUSTER1_BINARY=${WORKDIR}/bin/$(PLAT)/cluster1

CLUSTER2_SRC=${WORKDIR}/example/cluster2
CLUSTER2_BINARY=${WORKDIR}/bin/$(PLAT)/cluster2

CLUSTER3_SRC=${WORKDIR}/example/cluster3

all: clean cluster

hello:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} &&\
	cd ${HELLO_SRC} && go build -o ${HELLO_BINARY}

cluster:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} &&\
	cd ${CLUSTER1_SRC} && go build -o ${CLUSTER1_BINARY}

	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} &&\
	cd ${CLUSTER2_SRC} && go build -o ${CLUSTER2_BINARY}

test:
ifeq ($(OS),Windows_NT)

else ifeq ($(shell uname),Darwin)
	# ${HELLO_BINARY}
	-@killall cluster1 || true
	-@killall cluster2 || true
	${CLUSTER1_BINARY} -c ${CLUSTER1_SRC}/config.yaml &
	${CLUSTER2_BINARY} -c ${CLUSTER2_SRC}/config.yaml
else
	# ${HELLO_BINARY}
	-@killall cluster1 || true
	-@killall cluster2 || true
	${CLUSTER1_BINARY} -c ${CLUSTER1_SRC}/config.yaml &
	${CLUSTER2_BINARY} -c ${CLUSTER2_SRC}/config.yaml
endif

cluster1:
ifeq ($(OS),Windows_NT)

else ifeq ($(shell uname),Darwin)
	${WORKDIR}/bin/macosx/cluster1 -c ${CLUSTER1_SRC}/config.yaml
else
	${WORKDIR}/bin/linux/cluster1 -c ${CLUSTER1_SRC}/config.yaml
endif

cluster2:
ifeq ($(OS),Windows_NT)

else ifeq ($(shell uname),Darwin)
	${WORKDIR}/bin/macosx/cluster2 -c ${CLUSTER2_SRC}/config.yaml
else
	${WORKDIR}/bin/linux/cluster2 -c ${CLUSTER2_SRC}/config.yaml
endif


cluster3:
ifeq ($(OS),Windows_NT)

else ifeq ($(shell uname),Darwin)
	${WORKDIR}/bin/macosx/cluster2 -c ${CLUSTER3_SRC}/config.yaml
else
	${WORKDIR}/bin/linux/cluster2 -c ${CLUSTER3_SRC}/config.yaml
endif


gotool:
	go fmt ${HELLO_SRC}
	go vet ${HELLO_SRC}

	go fmt ${CLUSTER1_SRC}
	go vet ${CLUSTER1_SRC}

	go fmt ${CLUSTER2_SRC}
	go vet ${CLUSTER2_SRC}


ifeq (run,$(firstword $(MAKECMDGOALS)))
RUN_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
$(eval $ (RUN_ARGS):;@:)
endif
run: $(RUN_ARGS)

clean:
	@if [ -f ${HELLO_BINARY} ] ; then rm ${HELLO_BINARY} ; fi
	
# go install github.com/xxjwxc/gormt@master
gorm:
	gormt -H=127.0.0.1 --port=3306 -u=root -p=password \
	-d=database -b=table -s=true -o ${MASTER_SRC}/lib/db/mysql/model

td:
	go mod tidy

mc:
	go clean -modcache

tree:
	tree --dirsfirst -L 2