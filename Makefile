# SPDX-FileCopyrightText: Copyright (c) 2024 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
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
TARGETS := knavigator klient node-resource-exporter
CMD_DIR := ./cmd
OUTPUT_DIR := ./bin

IMAGE_REPO ?=docker.io/nvidia/knavigator
GIT_REF =$(shell git rev-parse --abbrev-ref HEAD)
IMAGE_TAG ?=$(GIT_REF)

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
	go test -coverprofile=coverage.out -covermode=atomic ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

# Lints the whole tree with .golangci.yml, the same scope as the Lint step of
# the check job in .github/workflows/go.yml, so a clean local run means a
# clean CI run.
.PHONY: lint
lint:
	$(LINTER_BIN) run ./...

# Fails if a file lacks the canonical SPDX header; the exclusion list is in
# the script. The script's own tests run first, so a broken check cannot
# pass unnoticed.
.PHONY: verify-boilerplate
verify-boilerplate:
	hack/verify-boilerplate_test.sh
	hack/verify-boilerplate.sh

.PHONY: mod
mod:
	go mod tidy

.PHONY: coverage
coverage: test
	go tool cover -func=coverage.out

.PHONY: image-build
image-build: build
	$(DOCKER_BIN) build -t $(IMAGE_REPO):$(IMAGE_TAG) -f ./Dockerfile .

.PHONY: image-push
image-push: image-build
	$(DOCKER_BIN) push $(IMAGE_REPO):$(IMAGE_TAG)
