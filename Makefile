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
ALMA8_IMAGE ?= almalinux:8.10
ALMA9_IMAGE ?= almalinux:9
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

.PHONY: all build plugins plugin musl test deb rpm deb-musl rpm-musl almalinux8 almalinux9 FORCE

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

almalinux8: ALMA_IMAGE := $(ALMA8_IMAGE)
almalinux8: ALMA_FLAVOR := almalinux8
almalinux9: ALMA_IMAGE := $(ALMA9_IMAGE)
almalinux9: ALMA_FLAVOR := almalinux9

almalinux8 almalinux9:
	$(DOCKER) build --platform $(RPM_PLATFORM) --build-arg GO_IMAGE=$(GO_IMAGE) --build-arg ALMA_IMAGE=$(ALMA_IMAGE) -f packaging/docker/almalinux.Dockerfile -t virtualsms-rpm-builder:$(ALMA_FLAVOR) .
	$(DOCKER) run --rm --platform $(RPM_PLATFORM) --user $$(id -u):$$(id -g) -e HOME=/tmp -e GOPROXY=$(CONTAINER_GOPROXY) -e GOCACHE=/src/build/.gocache-$(ALMA_FLAVOR) -e GOMODCACHE=/src/build/.gomodcache-$(ALMA_FLAVOR) -v "$$(pwd):/src" -w /src virtualsms-rpm-builder:$(ALMA_FLAVOR) make -B rpm BUILD_DIR=build/$(ALMA_FLAVOR) VERSION=$(VERSION) RPM_FLAVOR=$(ALMA_FLAVOR)

deb-musl:
	$(MAKE) build CC=musl-gcc BUILD_DIR=build/musl PLUGIN=
	bash packaging/package-deb.sh build/musl $(VERSION) musl

rpm-musl:
	$(MAKE) build CC=musl-gcc BUILD_DIR=build/musl PLUGIN=
	bash packaging/package-rpm.sh build/musl $(VERSION) musl
