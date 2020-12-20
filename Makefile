# This doesn't work for psqldef due to lib/pq
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static" -X main.version=$(shell git describe --tags --abbrev=0)'
GOVERSION=$(shell go version)
GOOS=$(word 1,$(subst /, ,$(lastword $(GOVERSION))))
GOARCH=$(word 2,$(subst /, ,$(lastword $(GOVERSION))))
BUILD_DIR=build/$(GOOS)-$(GOARCH)

.PHONY: all build clean deps package package-zip package-targz

all: build

build: deps
	mkdir -p $(BUILD_DIR)
	cd cmd/mysqldef && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mysqldef
	cd cmd/psqldef && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/psqldef
	cd cmd/sqlite3def && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/sqlite3def

clean:
	rm -rf build package

deps:
	go get -t ./...

package:
	$(MAKE) package-targz GOOS=linux GOARCH=amd64
	$(MAKE) package-targz GOOS=linux GOARCH=386
	$(MAKE) package-targz GOOS=linux GOARCH=arm64
	$(MAKE) package-targz GOOS=linux GOARCH=arm
	$(MAKE) package-zip GOOS=darwin GOARCH=amd64
	$(MAKE) package-zip GOOS=darwin GOARCH=386
	$(MAKE) package-zip GOOS=windows GOARCH=amd64
	$(MAKE) package-zip GOOS=windows GOARCH=386

package-zip: build
	mkdir -p package
	cd $(BUILD_DIR) && zip ../../package/mysqldef_$(GOOS)_$(GOARCH).zip mysqldef
	cd $(BUILD_DIR) && zip ../../package/psqldef_$(GOOS)_$(GOARCH).zip psqldef
	cd $(BUILD_DIR) && zip ../../package/sqlite3def_$(GOOS)_$(GOARCH).zip sqlite3def

package-targz: build
	mkdir -p package
	cd $(BUILD_DIR) && tar zcvf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef
	cd $(BUILD_DIR) && tar zcvf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef
	cd $(BUILD_DIR) && tar zcvf ../../package/sqlite3def_$(GOOS)_$(GOARCH).tar.gz sqlite3def

test: test-mysqldef test-psqldef test-sqlite3def test-sqlparser

test-mysqldef: deps
	cd cmd/mysqldef && go test

test-psqldef: deps
	cd cmd/psqldef && go test

test-sqlite3def: deps
	cd cmd/sqlite3def && go test

test-sqlparser:
	cd sqlparser && go test
