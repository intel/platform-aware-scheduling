BINARY_NAME=extender
GO_VERSION = 1.21-alpine
GOLICENSES_VERSION=v1.6.0

.PHONY: test

test:
		go test ./...  -v *_test.go

.PHONY: all

all:  format build

build:
		CGO_ENABLED=0 GO111MODULE=on go build -ldflags="-s -w" -o ./bin/$(BINARY_NAME) ./cmd
image:
		docker build --build-arg GOLICENSES_VERSION=$(GOLICENSES_VERSION) --build-arg GO_VERSION=$(GO_VERSION) -f deploy/images/Dockerfile ../ -t tasextender
format:
		gofmt -w -s .

clean:
		rm -f ./bin/$(BINARY_NAME)

e2e:
		cd ../.github/e2e; go test -v e2e_test.go

