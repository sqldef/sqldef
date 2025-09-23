FROM golang:1.25.1-bookworm AS builder

ARG SQLDEF_TOOL=mysqldef

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
