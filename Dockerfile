FROM golang:1.11.1-alpine3.8
run apk add --no-cache build-base make gcc  git; \
  git clone https://github.com/k0kubun/sqldef.git; \
  export GOPATH=/go/ ; \
  export GOBIN=$HOME/bin ; \
  cd sqldef; \
  make all;

WORKDIR "/go/sqldef/build/linux-amd64"
CMD ["/go/sqldef/build/linux-amd64/mysqldef"]
