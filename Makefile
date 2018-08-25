# This actually doesn't work due to lib/pq
# TODO: split drivers to different packages
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static"'

.PHONY: all clean deps
.PHONY: cmd/mysqldef/mysqldef cmd/psqldef/psqldef

all: cmd/mysqldef/mysqldef cmd/psqldef/psqldef

cmd/mysqldef/mysqldef: deps
	cd cmd/mysqldef && go build $(GOFLAGS)

cmd/psqldef/psqldef: deps
	cd cmd/psqldef && go build $(GOFLAGS)

deps:
	go get -t ./...

clean:
	rm -f cmd/mysqldef/mysqldef
	rm -f cmd/psqldef/psqldef
