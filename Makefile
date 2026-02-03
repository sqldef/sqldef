# This doesn't work for psqldef due to lib/pq
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s'
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BUILD_DIR=build/$(GOOS)-$(GOARCH)
SQLDEF=$(shell pwd)

# https://pkg.go.dev/golang.org/x/tools/cmd/goyacc
GOYACC_VERSION=v0.40.0

ifeq ($(GOOS), windows)
  SUFFIX=.exe
else
  SUFFIX=
endif

ifeq ($(VERBOSE), 1)
  GOTESTFLAGS := -v
endif

ifeq ($(CI), true)
  GOTEST := go test $(GOTESTFLAGS)
else
  GOTEST := go run gotest.tools/gotestsum@latest --hide-summary=skipped -- $(GOTESTFLAGS)
endif

all: build
.PHONY: all

build: build-mysqldef build-sqlite3def build-mssqldef build-psqldef
.PHONY: build

build-mysqldef:
	mkdir -p $(BUILD_DIR)
	cd cmd/mysqldef && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mysqldef$(SUFFIX)
.PHONY: build-mysqldef

build-sqlite3def:
	mkdir -p $(BUILD_DIR)
	cd cmd/sqlite3def && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/sqlite3def$(SUFFIX)
.PHONY: build-sqlite3def

build-mssqldef:
	mkdir -p $(BUILD_DIR)
	cd cmd/mssqldef && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/mssqldef$(SUFFIX)
.PHONY: build-mssqldef

build-psqldef:
	mkdir -p $(BUILD_DIR)
	cd cmd/psqldef && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o ../../$(BUILD_DIR)/psqldef$(SUFFIX)
.PHONY: build-psqldef

clean:
	rm -rf build package coverage.out coverage.xml
	rm -f cmd/mysqldef/mysqldef cmd/psqldef/psqldef cmd/sqlite3def/sqlite3def cmd/mssqldef/mssqldef
	rm -f cmd/mysqldef/mysqldef.exe cmd/psqldef/psqldef.exe cmd/sqlite3def/sqlite3def.exe cmd/mssqldef/mssqldef.exe
.PHONY: clean

update-deps:
	go get -u ./...
	go mod tidy
.PHONY: update-deps

package-zip: build
	mkdir -p package
	cd $(BUILD_DIR) && zip -9 ../../package/mssqldef_$(GOOS)_$(GOARCH).zip mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip -9 ../../package/mysqldef_$(GOOS)_$(GOARCH).zip mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && zip -9 ../../package/sqlite3def_$(GOOS)_$(GOARCH).zip sqlite3def$(SUFFIX)
	cd $(BUILD_DIR) && zip -9 ../../package/psqldef_$(GOOS)_$(GOARCH).zip psqldef$(SUFFIX)
.PHONY: package-zip

package-tar.gz: build
	mkdir -p package
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/mssqldef_$(GOOS)_$(GOARCH).tar.gz mssqldef$(SUFFIX)
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/mysqldef_$(GOOS)_$(GOARCH).tar.gz mysqldef$(SUFFIX)
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/sqlite3def_$(GOOS)_$(GOARCH).tar.gz sqlite3def$(SUFFIX)
	cd $(BUILD_DIR) && GZIP=-9 tar zcf ../../package/psqldef_$(GOOS)_$(GOARCH).tar.gz psqldef$(SUFFIX)
.PHONY: package-tar.gz

parser:
	go run golang.org/x/tools/cmd/goyacc@$(GOYACC_VERSION) -o parser/parser.go parser/parser.y
	gofmt -w ./parser/parser.go
.PHONY: parser

parser-v:
	go run golang.org/x/tools/cmd/goyacc@$(GOYACC_VERSION) -v y.output -o parser/parser.go parser/parser.y
	gofmt -w ./parser/parser.go
.PHONY: parser-v

test:
	$(GOTEST) $(GOTESTFLAGS) ./...
.PHONY: test

test-mysqldef:
	MYSQL_FLAVOR=$${MYSQL_FLAVOR:-mysql} $(GOTEST) ./cmd/mysqldef
.PHONY: test-mysqldef

test-psqldef:
	$(GOTEST) ./cmd/psqldef ./database/postgres
.PHONY: test-psqldef

test-sqlite3def:
	$(GOTEST) ./cmd/sqlite3def
.PHONY: test-sqlite3def

test-mssqldef:
	$(GOTEST) ./cmd/mssqldef ./database/mssql
.PHONY: test-mssqldef

test-core:
	$(GOTEST) ./parser ./schema ./util
.PHONY: test-core

test-example-offline:
	./example/run-offline.sh psqldef
	./example/run-offline.sh mysqldef
	./example/run-offline.sh sqlite3def
	./example/run-offline.sh mssqldef
.PHONY: test-example-offline

test-all-flavors: test
	MYSQL_FLAVOR=mariadb MYSQL_PORT=3307 $(GOTEST) ./cmd/mysqldef
	MYSQL_FLAVOR=tidb MYSQL_PORT=4000 $(GOTEST) ./cmd/mysqldef
	PG_FLAVOR=pgvector PGPORT=55432 $(GOTEST) ./cmd/psqldef
.PHONY: test-all-flavors

test-example:
	./example/run.sh psqldef
	./example/run.sh mysqldef
	./example/run.sh sqlite3def
	./example/run.sh mssqldef
.PHONY: test-example

test-cov:
	go test $(GOTESTFLAGS) -coverprofile=coverage.out -coverpkg=./... ./...
	@grep -v -e "parser.y" -e "parser/parser.go" -e "testutils.go" coverage.out > coverage_filtered.out
	@go tool cover -func=coverage_filtered.out
	@rm coverage_filtered.out
.PHONY: test-cov

test-cov-xml: test-cov
	grep -v -e "parser.y" -e "parser/parser.go" -e "testutils.go" coverage.out | go run github.com/boumenot/gocover-cobertura@latest > coverage.xml
.PHONY: test-cov-xml

format:
	go fmt ./...
.PHONY: format

lint:
	go vet ./...
.PHONY: lint

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
.PHONY: vulncheck

fix:
	go fix ./...
.PHONY: fix

touch:
	touch parser/parser.y
.PHONY: touch
