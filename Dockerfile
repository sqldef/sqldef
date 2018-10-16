FROM golang:1.11.1-alpine3.8
RUN apk add --no-cache build-base make gcc git

COPY . /sqldef
WORKDIR /sqldef
RUN export GOPATH=/go/; \
  export GOBIN=$HOME/bin; \
  make all && sh -ec "mv build/*/mysqldef /usr/bin/ && mv build/*/psqldef /usr/bin/"
