IMAGE_TAG = latest
ifneq ($(TAG),)
IMAGE_TAG = $(TAG)
endif

GO_VERSION = 1.21
GOLICENSES_VERSION?=v1.6.0

BUILD_OUTPUT_DIR?=./bin

ifneq ("$(wildcard licenses/)","")
LOCAL_LICENSES=TRUE
endif

GOVERSION=$(shell go version | sed 's/^[^0-9]*//' | cut -d' ' -f1)
BUILDDATE=$(shell date "+%Y%m%d-%T")
VERSION=$(shell git describe --tags --long --match configurator/v*)

LDFLAGS = \
-s -w \
-X main.version=$(VERSION) \
-X main.buildDate=$(BUILDDATE) \
-X main.goVersion=$(GOVERSION)

.PHONY: test all build format clean e2e lint

test:
	go test ./...  -v *_test.go

all: format build lint

lint: format build
	golangci-lint run

build:
	CGO_ENABLED=0 GO111MODULE=on go build -ldflags="$(LDFLAGS)" -o $(BUILD_OUTPUT_DIR)/configurator ./internal/cmd

update-baseimage:
	docker pull golang:$(GO_VERSION)

containerized-build: update-baseimage
	DOCKER_BUILDKIT=1 docker build -o - --build-arg GOLICENSES_VERSION=$(GOLICENSES_VERSION) \
	--build-arg GO_VERSION=$(GO_VERSION) --build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) \
	-f Dockerfile ../ -t $(IMAGE_PATH)pas-configurator:$(IMAGE_TAG) | tar x bin/configurator

image: update-baseimage
	docker build --build-arg GOLICENSES_VERSION=$(GOLICENSES_VERSION) \
	--build-arg GO_VERSION=$(GO_VERSION) --build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) \
	-f Dockerfile ../ -t $(IMAGE_PATH)pas-configurator:$(IMAGE_TAG)

format:
	gofmt -w -s .

clean: clean-licenses
	rm -f ./bin/*
	rm -rf vendor

clean-licenses:
	rm -rf licenses

licenses: clean-licenses
	GO111MODULE=on go run github.com/google/go-licenses@$(GOLICENSES_VERSION) \
	save "./internal/cmd" --save_path licenses

e2e:

