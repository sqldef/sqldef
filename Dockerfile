# must sync with go.mod
FROM golang:1.26.0-alpine AS builder

ARG SQLDEF_TOOL=mysqldef

# Install build dependencies
RUN apk add --no-cache make

WORKDIR /work

COPY go.mod go.sum .
RUN go mod download

COPY . .

RUN set -ex \
    && make build-$SQLDEF_TOOL \
    && build/$(go env GOOS)-$(go env GOARCH)/$SQLDEF_TOOL --version

FROM scratch

ARG SQLDEF_TOOL=mysqldef

COPY --from=builder /work/build/*/$SQLDEF_TOOL /usr/local/bin/sqldef

ENTRYPOINT ["sqldef"]
