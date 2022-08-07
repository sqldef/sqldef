# This doesn't work for psqldef due to lib/pq
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static" -X main.version=$(shell git describe --tags --abbrev=0)'
GOVERSION=$(shell go version)
GOOS=$(word 1,$(subst /, ,$(lastword $(GOVERSION))))
GOARCH=$(word 2,$(subst /, ,$(lastword $(GOVERSION))))
BUILD_DIR=build/$(GOOS)-$(GOARCH)
SHELL=/bin/bash
SQLDEF=$(shell pwd)
MACOS_VERSION := 11.3
ZIG_TARGET := $(shell uname -m)-$(shell uname -s | tr '[:upper:]' '[:lower:]')
ZIG_CFLAGS :=
CGO_LDFLAGS :=

.PHONY: all build clean deps package package-zip package-targz

all: build

MacOSX.sdk.tar.xz:
	wget https://github.com/phracker/MacOSX-SDKs/releases/download/$(MACOS_VERSION)/MacOSX$(MACOS_VERSION).sdk.tar.xz
	mv MacOSX$(MACOS_VERSION).sdk.tar.xz $@

MacOSX.sdk: MacOSX.sdk.tar.xz
	tar xf $<
	mv MacOSX$(MACOS_VERSION).sdk $@

build:
	mkdir -p $(BUILD_DIR)
	cd cmd/mysqldef    && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mysqldef
	cd cmd/sqlite3def  && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/sqlite3def
	cd cmd/mssqldef    && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mssqldef
	if [[ $(GOOS) != windows ]]; then \
		cd cmd/psqldef && CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) \
			CC="zig cc -target $(ZIG_TARGET) $(ZIG_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
			go build $(GOFLAGS) -o ../../$(BUILD_DIR)/psqldef; \
	fi;

clean:
	rm -rf build package

deps:
	go get -t ./...

package-zip: build
	mkdir -p package
	cd $(BUILD_DIR) && zip ../../package/mssqldef_$(GOOS)_$(GOARCH).zip mssqldef
	cd $(BUILD_DIR) && zip ../../package/mysqldef_$(GOOS)_$(GOARCH).zip mysqldef
	cd $(BUILD_DIR) && zip ../../package/sqlite3def_$(GOOS)_$(GOARCH).zip sqlite3def
	if [[ $(GOOS) != windows ]]; then \
		cd $(BUILD_DIR) && zip ../../package/psqldef_$(GOOS)_$(GOARCH).zip psqldef; \
	fi

package-tar.gz: build
	mkdir -p package
	cd $(BUILD_DIR) && tar zcvf ../../package/mssqldef_$(GOOS)_$(GOARCH).tar.gz mssqldef
	cd $(BUILD_DIR) && tar zcvf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef
	cd $(BUILD_DIR) && tar zcvf ../../package/sqlite3def_$(GOOS)_$(GOARCH).tar.gz sqlite3def
	if [[ $(GOOS) != windows ]]; then \
		cd $(BUILD_DIR) && tar zcvf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef; \
	fi

test: test-mysqldef test-psqldef test-sqlite3def test-mssqldef

test-mysqldef:
	cd cmd/mysqldef && go test

test-psqldef:
	cd cmd/psqldef && go test

test-sqlite3def:
	cd cmd/sqlite3def && go test

test-mssqldef:
	cd cmd/mssqldef && go test
