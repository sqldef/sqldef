FROM  golang:1.24.1-bookworm as builder

WORKDIR /work
COPY . .

RUN go install
RUN make
RUN find .

FROM alpine as final

RUN mkdir -p /usr/local/bin
COPY --from=builder /work/build/*/* /usr/local/bin

