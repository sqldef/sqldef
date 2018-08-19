GOFLAGS := -tags netgo -installsuffix netgo -ldflags '-w -s --extldflags "-static"'

.PHONY: all
all: schemasql

schemasql: main.go
	go build -o $@ $(GOFLAGS)
