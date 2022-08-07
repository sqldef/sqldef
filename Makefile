# This doesn't work for psqldef due to lib/pq
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static" -X main.version=$(shell git describe --tags --abbrev=0)'
GOVERSION=$(shell go version)
GOOS=$(word 1,$(subst /, ,$(lastword $(GOVERSION))))
GOARCH=$(word 2,$(subst /, ,$(lastword $(GOVERSION))))
BUILD_DIR=build/$(GOOS)-$(GOARCH)
SHELL=/bin/bash

# Because ghr doesn't support cross-compiling, cgo cross-build of mattn/go-sqlite3 is failing.
# We should use https://github.com/karalabe/xgo or something to support sqlite3def in non-Linux OSes.
SQLITE3_OS=linux

.PHONY: all build clean deps package package-zip package-targz

all: build

build:
	mkdir -p $(BUILD_DIR)
	cd cmd/mysqldef && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mysqldef
	cd cmd/psqldef && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/psqldef
	cd cmd/sqlite3def && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/sqlite3def
	cd cmd/mssqldef && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mssqldef

clean:
	rm -rf build package

deps:
	go get -t ./...

package: deps
	pids=(); \
	$(MAKE) package-targz GOOS=linux   GOARCH=amd64 & pids+=($$!); \
	$(MAKE) package-targz GOOS=linux   GOARCH=386   & pids+=($$!); \
	$(MAKE) package-targz GOOS=linux   GOARCH=arm64 & pids+=($$!); \
	$(MAKE) package-targz GOOS=linux   GOARCH=arm   & pids+=($$!); \
	$(MAKE) package-zip   GOOS=darwin  GOARCH=amd64 & pids+=($$!); \
	$(MAKE) package-zip   GOOS=darwin  GOARCH=arm64 & pids+=($$!); \
	$(MAKE) package-zip   GOOS=windows GOARCH=amd64 & pids+=($$!); \
	$(MAKE) package-zip   GOOS=windows GOARCH=386   & pids+=($$!); \
	wait $${pids[@]}

package-zip: build
	mkdir -p package
	cd $(BUILD_DIR) && zip ../../package/mssqldef_$(GOOS)_$(GOARCH).zip mssqldef
	cd $(BUILD_DIR) && zip ../../package/mysqldef_$(GOOS)_$(GOARCH).zip mysqldef
	cd $(BUILD_DIR) && zip ../../package/psqldef_$(GOOS)_$(GOARCH).zip psqldef
	if [ "$(GOOS)" = "$(SQLITE3_OS)" ]; then \
		cd $(BUILD_DIR) && zip ../../package/sqlite3def_$(GOOS)_$(GOARCH).zip sqlite3def; \
	fi

package-targz: build
	mkdir -p package
	cd $(BUILD_DIR) && tar zcvf ../../package/mssqldef_$(GOOS)_$(GOARCH).tar.gz mssqldef
	cd $(BUILD_DIR) && tar zcvf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef
	cd $(BUILD_DIR) && tar zcvf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef
	if [ "$(GOOS)" = "$(SQLITE3_OS)" ]; then \
		cd $(BUILD_DIR) && tar zcvf ../../package/sqlite3def_$(GOOS)_$(GOARCH).tar.gz sqlite3def; \
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
