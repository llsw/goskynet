include platform.mk
.PHONY: all help test hello clean

WORKDIR=$(shell pwd)
HELLO_SRC=${WORKDIR}/example/hello
HELLO_BINARY=${WORKDIR}/bin/$(PLAT)/hello

all: clean hello test

aes:
	$(CC) $(AES_SRC) $(SHARED) -o $(AESOUT)
hello:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} &&\
	cd ${HELLO_SRC} && go build -o ${HELLO_BINARY}

test:
ifeq ($(OS),Windows_NT)

else ifeq ($(shell uname),Darwin)
	${HELLO_BINARY}
else
	${HELLO_BINARY}
endif

gotool:
	go fmt ${HELLO_SRC}
	go vet ${HELLO_SRC}

clean:
	@if [ -f ${HELLO_BINARY} ] ; then rm ${HELLO_BINARY} ; fi