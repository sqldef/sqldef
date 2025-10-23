# This doesn't work for psqldef due to lib/pq
VERSION := $(shell cat VERSION)
REVISION := $(shell git describe --always)
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s -X main.version=$(VERSION) -X main.revision=$(REVISION)'
GOVERSION=$(shell go version)
GOOS=$(word 1,$(subst /, ,$(lastword $(GOVERSION))))
GOARCH=$(word 2,$(subst /, ,$(lastword $(GOVERSION))))
BUILD_DIR=build/$(GOOS)-$(GOARCH)
SQLDEF=$(shell pwd)
MACOS_VERSION := 11.3

ifeq ($(GOOS), windows)
  SUFFIX=.exe
else
  SUFFIX=
endif

ifeq ($(VERBOSE), 1)
  GOTESTFLAGS := -v
endif

.PHONY: all build clean deps goyacc package package-zip package-targz parser parser-v build-mysqldef build-sqlite3def build-mssqldef build-psqldef test-cov test-cov-xml

all: build

build: build-mysqldef build-sqlite3def build-mssqldef build-psqldef

build-mysqldef:
	mkdir -p $(BUILD_DIR)
	cd cmd/mysqldef && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mysqldef$(SUFFIX)

build-sqlite3def:
	mkdir -p $(BUILD_DIR)
	cd cmd/sqlite3def && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/sqlite3def$(SUFFIX)

build-mssqldef:
	mkdir -p $(BUILD_DIR)
	cd cmd/mssqldef && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mssqldef$(SUFFIX)

build-psqldef:
	mkdir -p $(BUILD_DIR)
	cd cmd/psqldef && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/psqldef$(SUFFIX)

clean:
	rm -rf build package coverage coverage.out coverage.xml
	rm -f cmd/mysqldef/mysqldef cmd/psqldef/psqldef cmd/sqlite3def/sqlite3def cmd/mssqldef/mssqldef
	rm -f cmd/mysqldef/mysqldef.exe cmd/psqldef/psqldef.exe cmd/sqlite3def/sqlite3def.exe cmd/mssqldef/mssqldef.exe

deps:
	go get -t ./...

goyacc:
	@if ! which goyacc > /dev/null; then \
	  go install golang.org/x/tools/cmd/goyacc@latest; \
	fi

package-zip: build
	mkdir -p package
	cd $(BUILD_DIR) && zip -9 ../../package/mssqldef_$(GOOS)_$(GOARCH).zip mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip -9 ../../package/mysqldef_$(GOOS)_$(GOARCH).zip mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip -9 ../../package/sqlite3def_$(GOOS)_$(GOARCH).zip sqlite3def$(SUFFIX)
	cd $(BUILD_DIR) && zip -9 ../../package/psqldef_$(GOOS)_$(GOARCH).zip psqldef$(SUFFIX)

package-tar.gz: build
	mkdir -p package
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/mssqldef_$(GOOS)_$(GOARCH).tar.gz mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/sqlite3def_$(GOOS)_$(GOARCH).tar.gz sqlite3def$(SUFFIX)
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef$(SUFFIX)

parser: goyacc
	goyacc -o parser/parser.go parser/parser.y
	gofmt -w ./parser/parser.go

parser-v: goyacc
	goyacc -v y.output -o parser/parser.go parser/parser.y
	gofmt -w ./parser/parser.go

test: test-mysqldef test-psqldef test-sqlite3def test-mssqldef

test-mysqldef:
	MYSQL_FLAVOR=$${MYSQL_FLAVOR:-mysql} go test $(GOTESTFLAGS) ./cmd/mysqldef

test-psqldef:
	go test $(GOTESTFLAGS) ./cmd/psqldef
	go test $(GOTESTFLAGS) ./database/postgres

test-sqlite3def:
	go test $(GOTESTFLAGS) ./cmd/sqlite3def

test-mssqldef:
	go test $(GOTESTFLAGS) ./cmd/mssqldef
	go test $(GOTESTFLAGS) ./database/mssql

test-cov:
	mkdir -p coverage
	GOCOVERDIR=$(shell pwd)/coverage go test $(GOTESTFLAGS) -count=1 ./...
	go tool covdata textfmt -i=coverage -o coverage.out
	@grep -v -e "parser.y" -e "parser/parser.go" -e "testutils.go" coverage.out > coverage_filtered.out
	@go tool cover -func=coverage_filtered.out
	@rm coverage_filtered.out

test-cov-xml: test-cov
	grep -v -e "parser.y" -e "parser/parser.go" -e "testutils.go" coverage.out | go run github.com/boumenot/gocover-cobertura@latest > coverage.xml

format:
	go fmt ./...

lint:
	go vet ./...

modernize:
	go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix ./...

touch:
	touch parser/parser.y
