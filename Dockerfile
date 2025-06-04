FROM golang:1.23.9-alpine3.21

ENV GOPROXY="https://proxy.golang.org"
ENV GO111MODULE="on"

ENV PORT=8080
EXPOSE 8080

ENV RAFT_PORT=8081
EXPOSE 8081

WORKDIR /go/src/github.com/TheOctave/kv-store-golang

RUN apk add --no-cache git
COPY . .

RUN go build -v -o /go/bin/server .

CMD ["/go/bin/server"]