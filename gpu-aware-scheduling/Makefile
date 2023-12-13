ifneq ($(TAG),)
IMAGE_TAG=:$(TAG)
endif

GO_VERSION = 1.21
GOLICENSES_VERSION?=v1.6.0
BUILD_OUTPUT_DIR?=./bin

ifneq ("$(wildcard licenses/)","")
LOCAL_LICENSES=TRUE
endif

GOVERSION=$(shell go version | sed 's/^[^0-9]*//' | cut -d' ' -f1)
BUILDDATE=$(shell date "+%Y%m%d-%T")
VERSION=$(shell git describe --tags --long --match gpu-aware-scheduling/v*)

LDFLAGS = \
-s -w \
-X main.version=$(VERSION) \
-X main.buildDate=$(BUILDDATE) \
-X main.goVersion=$(GOVERSION)


.PHONY: test all build image release-image format clean licenses mock e2e lint update-baseimage

test:
	go test ./...  -v *_test.go

all: format build lint

lint: format build
	golangci-lint run

build:
	CGO_ENABLED=0 GO111MODULE=on go build -ldflags="$(LDFLAGS)" -o $(BUILD_OUTPUT_DIR)/extender ./cmd/gas-scheduler-extender

update-baseimage:
	docker pull golang:$(GO_VERSION)

#note: you can speed up subsequent docker builds by doing "go mod vendor"
#note: you can further speed up subsequent docker builds by doing "make licenses"
image: update-baseimage
	docker build --build-arg GOLICENSES_VERSION=$(GOLICENSES_VERSION) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) --build-arg GO_VERSION=$(GO_VERSION) \
	-f deploy/images/Dockerfile ../ -t $(IMAGE_PATH)gpu-extender$(IMAGE_TAG)

release-image: clean update-baseimage
	docker build --build-arg GOLICENSES_VERSION=$(GOLICENSES_VERSION) --build-arg GO_VERSION=$(GO_VERSION) \
	-f deploy/images/Dockerfile ../ -t $(IMAGE_PATH)gpu-extender$(IMAGE_TAG)

format:
	gofmt -w -s .

clean: clean-licenses
	rm -f ./bin/*
	rm -rf vendor

clean-licenses:
	rm -rf licenses

licenses: clean-licenses
	GO111MODULE=on go run github.com/google/go-licenses@$(GOLICENSES_VERSION) \
	save "./cmd/gas-scheduler-extender" --save_path licenses

mock:
	mockery --name=CacheAPI --dir=pkg/gpuscheduler --inpackage --note="+build !validation\nre-generate with 'make mock'"
	mockery --name=InternalCacheAPI --dir=pkg/gpuscheduler --inpackage --note="+build !validation\nre-generate with 'make mock'"

e2e:

