# This actually doesn't work due to lib/pq
# TODO: split drivers to different packages
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static"'

.PHONY: all
all: cmd/mysqldef/mysqldef cmd/psqldef/psqldef

.PHONY: cmd/mysqldef/mysqldef
cmd/mysqldef/mysqldef:
	cd cmd/mysqldef && go build $(GOFLAGS)

.PHONY: cmd/psqldef/psqldef
cmd/psqldef/psqldef:
	cd cmd/psqldef && go build $(GOFLAGS)
