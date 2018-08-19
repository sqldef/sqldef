GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static"'

.PHONY: all
all: schemasql

.PHONY: schemasql
schemasql:
	go build -o $@ $(GOFLAGS)
