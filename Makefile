BINARY_NAME_1=controller
BINARY_NAME_2=extender

.PHONY: test

test:
		go test ./...  -v *_test.go

.PHONY: all

all:  format build

build:
		CGO_ENABLED=0 GO111MODULE=on go build -ldflags="-s -w" -o ./bin/$(BINARY_NAME_1) ./cmd/tas-policy-controller
		CGO_ENABLED=0 GO111MODULE=on go build -ldflags="-s -w" -o ./bin/$(BINARY_NAME_2) ./cmd/tas-scheduler-extender

image:
	   docker build -f deploy/images/Dockerfile_extender bin/ -t tas-extender
	   docker build -f deploy/images/Dockerfile_controller bin/ -t tas-controller

format:
		gofmt -w -s .

clean:
		rm -f ./bin/$(BINARY_NAME_1)
		rm -f ./bin/$(BINARY_NAME_2)


