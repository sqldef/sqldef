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

.PHONY: all build clean deps goyacc package package-zip package-targz parser

all: build

build:
	mkdir -p $(BUILD_DIR)
	cd cmd/mysqldef    && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mysqldef$(SUFFIX)
	cd cmd/sqlite3def  && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/sqlite3def$(SUFFIX)
	cd cmd/mssqldef    && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mssqldef$(SUFFIX)
	cd cmd/psqldef     && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/psqldef$(SUFFIX)	

clean:
	rm -rf build package

deps:
	go get -t ./...

goyacc:
	@if ! which goyacc > /dev/null; then \
	  go install golang.org/x/tools/cmd/goyacc; \
	fi

package-zip: build
	mkdir -p package
	cd $(BUILD_DIR) && zip ../../package/mssqldef_$(GOOS)_$(GOARCH).zip mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip ../../package/mysqldef_$(GOOS)_$(GOARCH).zip mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip ../../package/sqlite3def_$(GOOS)_$(GOARCH).zip sqlite3def$(SUFFIX)
	cd $(BUILD_DIR) && zip ../../package/psqldef_$(GOOS)_$(GOARCH).zip psqldef$(SUFFIX)

package-tar.gz: build
	mkdir -p package
	cd $(BUILD_DIR) && tar zcvf ../../package/mssqldef_$(GOOS)_$(GOARCH).tar.gz mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && tar zcvf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && tar zcvf ../../package/sqlite3def_$(GOOS)_$(GOARCH).tar.gz sqlite3def$(SUFFIX)
	cd $(BUILD_DIR) && tar zcvf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef$(SUFFIX)

# Cached
parser: goyacc parser/parser.go

parser/parser.go: parser/parser.y
	goyacc -o parser/parser.go parser/parser.y
	gofmt -w parser/parser.go

test: test-mysqldef test-psqldef test-sqlite3def test-mssqldef

test-mysqldef:
	go test -v ./cmd/mysqldef

test-psqldef:
	go test -v ./cmd/psqldef
	go test -v ./database/postgres

test-sqlite3def:
	go test -v ./cmd/sqlite3def

test-mssqldef:
	go test -v ./cmd/mssqldef
	go test -v ./database/mssql

touch:
	touch parser/parser.y
