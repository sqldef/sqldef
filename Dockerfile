FROM  golang:1.24.1-bookworm as builder

WORKDIR /work
COPY . .

RUN go install
RUN uname
RUN make && build/$(go env GOOS)-$(go env GOARCH)/sqlite3def --version

FROM alpine as final

RUN mkdir -p /usr/local/bin
COPY --from=builder /work/build/*/* /usr/local/bin