# This actually doesn't work due to lib/pq
GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static"'

.PHONY: all
all: sqldef

.PHONY: sqldef
sqldef:
	go build -o $@ $(GOFLAGS)
