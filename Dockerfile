# must sync with go.mod
FROM golang:1.27.0-alpine AS builder

ARG SQLDEF_TOOL=mysqldef

# Install build dependencies
RUN apk add --no-cache make

WORKDIR /work

COPY go.mod go.sum .
RUN go mod download

COPY . .

# parser.go is generated and is not carried by pull requests, so build it here
# instead of trusting whatever is in the build context.
RUN set -ex \
    && make parser \
    && make build-$SQLDEF_TOOL \
    && build/$(go env GOOS)-$(go env GOARCH)/$SQLDEF_TOOL --version

FROM scratch

ARG SQLDEF_TOOL=mysqldef

COPY --from=builder /work/build/*/$SQLDEF_TOOL /usr/local/bin/sqldef

ENTRYPOINT ["sqldef"]
