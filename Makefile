GO ?= go
CC ?= cc
DOCKER ?= docker
VERSION ?= 0.1.0
BUILD_DIR ?= build/native
RPM_FLAVOR ?= native
RPM_PLATFORM ?= linux/amd64
GO_VERSION ?= 1.26.0
GO_IMAGE ?= golang:$(GO_VERSION)
CONTAINER_GOPROXY ?= https://goproxy.cn,direct
PLUGIN_NAMES := $(patsubst plugins/%/main.go,%,$(wildcard plugins/*/main.go))
SELECTED_PLUGINS := $(if $(PLUGIN),$(PLUGIN),$(PLUGIN_NAMES))
ALL_PLUGIN_TARGETS := $(addprefix $(BUILD_DIR)/plugins/,$(addsuffix .so,$(PLUGIN_NAMES)))
SELECTED_PLUGIN_TARGETS := $(addprefix $(BUILD_DIR)/plugins/,$(addsuffix .so,$(SELECTED_PLUGINS)))
PLUGIN_SHARED_SOURCES := $(shell find internal pkg -name '*.go')

ifneq ($(PLUGIN),)
ifeq ($(filter $(PLUGIN),$(PLUGIN_NAMES)),)
$(error unknown plugin "$(PLUGIN)"; available: $(PLUGIN_NAMES))
endif
endif

.PHONY: all build plugins plugin musl test deb rpm deb-musl rpm-musl rpm-almalinux8 FORCE

all: build

build: $(BUILD_DIR)/virtualsms plugins

plugins: $(SELECTED_PLUGIN_TARGETS)

plugin:
	@test -n "$(PLUGIN)" || { echo "usage: make plugin PLUGIN=<name>" >&2; exit 2; }
	@$(MAKE) plugins PLUGIN=$(PLUGIN) CC=$(CC) BUILD_DIR=$(BUILD_DIR)

$(BUILD_DIR)/virtualsms: $(shell find cmd internal pkg -name '*.go') go.mod go.sum Makefile
	@mkdir -p $(dir $@)
	CGO_ENABLED=1 CC=$(CC) $(GO) build -o $@ ./cmd/virtualsms

define plugin_rule
$(BUILD_DIR)/plugins/$(1).so: $(wildcard plugins/$(1)/*.go) $(PLUGIN_SHARED_SOURCES) go.mod go.sum Makefile $(BUILD_DIR)/plugins/.version
	@mkdir -p $(BUILD_DIR)/plugins
	CGO_ENABLED=1 CC=$(CC) $(GO) build -buildmode=plugin -ldflags "-X main.ProviderVersion=$(VERSION)" -o $$@ ./plugins/$(1)
endef

$(BUILD_DIR)/plugins/.version: FORCE
	@mkdir -p $(dir $@)
	@test -f $@ && test "$$(cat $@)" = "$(VERSION)" || printf '%s\n' "$(VERSION)" > $@

$(foreach name,$(PLUGIN_NAMES),$(eval $(call plugin_rule,$(name))))

musl:
	$(MAKE) build CC=musl-gcc BUILD_DIR=build/musl

test:
	CGO_ENABLED=1 CC=$(CC) $(GO) test ./...

deb: $(BUILD_DIR)/virtualsms $(ALL_PLUGIN_TARGETS)
	bash packaging/package-deb.sh $(BUILD_DIR) $(VERSION) native

rpm: $(BUILD_DIR)/virtualsms $(ALL_PLUGIN_TARGETS)
	bash packaging/package-rpm.sh $(BUILD_DIR) $(VERSION) $(RPM_FLAVOR)

rpm-almalinux8:
	$(DOCKER) build --platform $(RPM_PLATFORM) --build-arg GO_IMAGE=$(GO_IMAGE) -f packaging/docker/almalinux-8.10.Dockerfile -t virtualsms-rpm-builder:almalinux-8.10 .
	$(DOCKER) run --rm --platform $(RPM_PLATFORM) --user $$(id -u):$$(id -g) -e HOME=/tmp -e GOPROXY=$(CONTAINER_GOPROXY) -e GOCACHE=/src/build/.gocache-almalinux8 -e GOMODCACHE=/src/build/.gomodcache-almalinux8 -v "$$(pwd):/src" -w /src virtualsms-rpm-builder:almalinux-8.10 make -B rpm BUILD_DIR=build/almalinux8.10 VERSION=$(VERSION) RPM_FLAVOR=almalinux8

deb-musl:
	$(MAKE) build CC=musl-gcc BUILD_DIR=build/musl PLUGIN=
	bash packaging/package-deb.sh build/musl $(VERSION) musl

rpm-musl:
	$(MAKE) build CC=musl-gcc BUILD_DIR=build/musl PLUGIN=
	bash packaging/package-rpm.sh build/musl $(VERSION) musl
