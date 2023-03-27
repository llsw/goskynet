PLAT ?= none
PLATS = linux macosx

CC := gcc

.PHONY : none $(PLATS) clean all cleanall help

ifneq ($(PLAT), none)

.PHONY : default

default :
	$(MAKE) $(PLAT)

endif

help:
	@echo "make linux/macosx 编译生成二进制文件"
	@echo "make test - 运行test程序"

none :
	$(MAKE) help

linux : PLAT = linux
linux : GOOS = linux
macosx : PLAT = macosx
macosx : GOOS = darwin
linux macosx : GOARCH = amd64

# Turn off jemalloc and malloc hook on macosx

linux:
ifeq ($(OS),Windows_NT)
else ifeq ($(shell uname),Darwin)
else
endif
	$(MAKE) all PLAT=$@ GOOS="$(GOOS)" GOARCH="$(GOARCH)"
macosx:
	$(MAKE) all PLAT=$@ GOOS="$(GOOS)" GOARCH="$(GOARCH)"

	
