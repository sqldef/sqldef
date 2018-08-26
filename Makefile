# This doesn't work for psqldef due to lib/pq
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static"'
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

package-targz: build
	mkdir -p package
	cd $(BUILD_DIR) && tar zcvf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef
	cd $(BUILD_DIR) && tar zcvf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef

test: test-mysqldef test-psqldef test-sqlparser

test-mysqldef: deps
	cd cmd/mysqldef && go test

test-psqldef: deps
	cd cmd/psqldef && go test

test-sqlparser:
	cd sqlparser && go test
