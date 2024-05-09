# Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

LINTER_BIN ?= golangci-lint
DOCKER_BIN ?= docker
TARGETS := knavigator
CMD_DIR := ./cmd
OUTPUT_DIR := ./bin

IMAGE_REPO ?=docker.io/nvidia/knavigator
GIT_REF =$(shell git rev-parse --abbrev-ref HEAD)
IMAGE_TAG ?=$(GIT_REF)

## verify: Verify code
.PHONY: verify
verify:
	@./hack/verify-all.sh

## update: Update all the generated
.PHONY: update
update:
	@./hack/update-all.sh

.PHONY: build
build:
	@for target in $(TARGETS); do        \
	  echo "Building $${target}";        \
	  CGO_ENABLED=0 go build -a          \
	    -o $(OUTPUT_DIR)/$${target}      \
	    -ldflags '-extldflags "-static"' \
	    $(CMD_DIR)/$${target};           \
	done

.PHONY: clean
clean:
	@for target in $(TARGETS); do             \
	  echo "rm -f $(OUTPUT_DIR)/$${target}";  \
	  rm -f $(OUTPUT_DIR)/$${target};         \
	done

.PHONY: test
test:
	@echo running tests
	go test ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	$(LINTER_BIN) run --new-from-rev "HEAD~$(git rev-list master.. --count)" ./...

.PHONY: mod
mod:
	go mod tidy

.PHONY: image-build
image-build: build
	$(DOCKER_BIN) build -t $(IMAGE_REPO):$(IMAGE_TAG) -f ./Dockerfile .

.PHONY: image-push
image-push: image-build
	$(DOCKER_BIN) push $(IMAGE_REPO):$(IMAGE_TAG)

.PRECIOUS: %.cast
%.cast: %.demo
	@WORK_DIR=$(shell dirname $<) \
	./hack/democtl.sh "$<" "$@" \
		--ps1='\033[1;96m~/nvidia/knavigator\033[1;94m$$\033[0m '

.PRECIOUS: %.svg
%.svg: %.cast
	@./hack/democtl.sh "$<" "$@" \
		--term xresources \
	  	--profile ./.xresources

%.mp4: %.cast
	@./hack/democtl.sh "$<" "$@"
