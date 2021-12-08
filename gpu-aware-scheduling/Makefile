ifneq ($(TAG),)
IMAGE_TAG=:$(TAG)
endif

.PHONY: test all build gpu-extender images format clean

test:
	go test ./...  -v *_test.go

all: format build

build:
	CGO_ENABLED=0 GO111MODULE=on go build -ldflags="-s -w" -o ./bin/extender ./cmd/gas-scheduler-extender

image: gpu-extender
	docker build -f deploy/images/Dockerfile_gpu-extender ../ -t $(IMAGE_PATH)gpu-extender$(IMAGE_TAG)

format:
	gofmt -w -s .

clean:
	rm -f ./bin/*

mock:
	mockery --name=CacheAPI --dir=pkg/gpuscheduler --inpkg --note="+build !validation\nre-generate with 'make mock'"
	mockery --name=InternalCacheAPI --dir=pkg/gpuscheduler --inpkg --note="+build !validation\nre-generate with 'make mock'"

e2e:

