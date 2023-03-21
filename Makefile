# This doesn't work for psqldef due to lib/pq
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s -X main.version=$(shell git describe --tags --abbrev=0)'
GOVERSION=$(shell go version)
GOOS=$(word 1,$(subst /, ,$(lastword $(GOVERSION))))
GOARCH=$(word 2,$(subst /, ,$(lastword $(GOVERSION))))
BUILD_DIR=build/$(GOOS)-$(GOARCH)
SHELL=/bin/bash
SQLDEF=$(shell pwd)
MACOS_VERSION := 11.3

ifeq ($(GOOS), windows)
  SUFFIX=.exe
else
  SUFFIX=
endif

.PHONY: all build clean deps package package-zip package-targz

all: build

build:
	mkdir -p $(BUILD_DIR)
	cd cmd/mysqldef    && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mysqldef$(SUFFIX)
	cd cmd/sqlite3def  && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/sqlite3def$(SUFFIX)
	cd cmd/mssqldef    && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mssqldef$(SUFFIX)
	if [[ $(GOOS) != windows ]]; then \
		cd cmd/psqldef && CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/psqldef$(SUFFIX); \
	fi;

clean:
	rm -rf build package

deps:
	go get -t ./...

package-zip: build
	mkdir -p package
	cd $(BUILD_DIR) && zip ../../package/mssqldef_$(GOOS)_$(GOARCH).zip mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip ../../package/mysqldef_$(GOOS)_$(GOARCH).zip mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip ../../package/sqlite3def_$(GOOS)_$(GOARCH).zip sqlite3def$(SUFFIX)
	if [[ $(GOOS) != windows ]]; then \
		cd $(BUILD_DIR) && zip ../../package/psqldef_$(GOOS)_$(GOARCH).zip psqldef$(SUFFIX); \
	fi

package-tar.gz: build
	mkdir -p package
	cd $(BUILD_DIR) && tar zcvf ../../package/mssqldef_$(GOOS)_$(GOARCH).tar.gz mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && tar zcvf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && tar zcvf ../../package/sqlite3def_$(GOOS)_$(GOARCH).tar.gz sqlite3def$(SUFFIX)
	if [[ $(GOOS) != windows ]]; then \
		cd $(BUILD_DIR) && tar zcvf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef$(SUFFIX); \
	fi

test: test-mysqldef test-psqldef test-sqlite3def test-mssqldef

test-mysqldef:
	go test -v ./cmd/mysqldef

test-psqldef:
	if [[ $(GOOS) != windows ]]; then \
		CGO_ENABLED=1 go test -v ./cmd/psqldef ./database/postgres; \
	else \
		go test -v ./cmd/psqldef ./database/postgres; \
	fi

test-sqlite3def:
	go test -v ./cmd/sqlite3def

test-mssqldef:
	go test -v ./cmd/mssqldef
