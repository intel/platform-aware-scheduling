ifneq ($(TAG),)
IMAGE_TAG=:$(TAG)
endif

GOLICENSES_VERSION?=v1.2.0

ifneq ("$(wildcard licenses/)","")
LOCAL_LICENSES=TRUE
endif

.PHONY: test all build image release-image format clean licenses mock e2e lint

test:
	go test ./...  -v *_test.go

all: format build lint

lint: format build
	golangci-lint run

build:
	CGO_ENABLED=0 GO111MODULE=on go build -ldflags="-s -w" -o ./bin/extender ./cmd/gas-scheduler-extender

#note: you can speed up subsequent docker builds by doing "go mod vendor"
#note: you can further speed up subsequent docker builds by doing "make licenses"
image:
	docker build --build-arg GOLICENSES_VERSION=$(GOLICENSES_VERSION) --build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) \
	-f deploy/images/Dockerfile_gpu-extender ../ -t $(IMAGE_PATH)gpu-extender$(IMAGE_TAG)

release-image: clean
	docker build --build-arg GOLICENSES_VERSION=$(GOLICENSES_IN_GO_MOD) -f deploy/images/Dockerfile_gpu-extender ../ \
	-t $(IMAGE_PATH)gpu-extender$(IMAGE_TAG)

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
	mockery --name=CacheAPI --dir=pkg/gpuscheduler --inpkg --note="+build !validation\nre-generate with 'make mock'"
	mockery --name=InternalCacheAPI --dir=pkg/gpuscheduler --inpkg --note="+build !validation\nre-generate with 'make mock'"

e2e:

