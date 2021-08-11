# generate version number
version=$(shell git describe --tags --long --always --dirty|sed 's/^v//')
objects = mq2nagcmd send_check

all: $(objects)
	-@go fmt

.PHONY: *.go
%: %.go
	go build -ldflags "-X main.version=$(version)" $@.go


static:
	go build -ldflags "-X main.version=$(version) -extldflags \"-static\"" -o $(binfile).static $(binfile).go

version:
	@echo $(version)
