# generate version number
version=$(shell git describe --tags --long --always --dirty|sed 's/^v//')
objects = mq2nagcmd

all: vendor $(objects) | glide.lock
	-@go fmt

.PHONY: *.go
%: %.go
	go build -ldflags "-X main.version=$(version)" $(objects).go


static: glide.lock vendor
	go build -ldflags "-X main.version=$(version) -extldflags \"-static\"" -o $(binfile).static $(binfile).go

clean:
	rm -rf vendor
	rm -rf _vendor
vendor: glide.lock
	glide install && touch vendor
glide.lock: glide.yaml
	glide update && touch glide.lock
glide.yaml:
version:
	@echo $(version)
