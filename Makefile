# generate version number
version=$(shell git describe --tags --long --always --dirty|sed 's/^v//')
# CGO_EXTLDFLAGS is added for cross-compiling purpose

all:
	-@go fmt
	go build -ldflags "$(CGO_EXTLDFLAGS) -X main.version=$(version)" -o mq2nagcmd cmd/mq2nagcmd/*.go
	go build -ldflags "$(CGO_EXTLDFLAGS) -X main.version=$(version)" -o send_check cmd/send_check/*.go


static:
	go build -ldflags "$(CGO_EXTLDFLAGS) -X main.version=$(version) -extldflags \"static\"" -o mq2nagcmd.static cmd/mq2nagcmd/*.go
	go build -ldflags "$(CGO_EXTLDFLAGS) -X main.version=$(version) -extldflags \"static\"" -o send_check.static cmd/send_check/*.go

version:
	@echo $(version)
