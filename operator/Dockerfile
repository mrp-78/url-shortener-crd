# Build the manager binary
FROM golang:alpine AS builder
WORKDIR /workspace

ENV GOPROXY="https://goproxy.io,direct"
ENV GOSUMDB=off

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -o manager cmd/main.go

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /workspace/manager /manager
USER 65532:65532

ENTRYPOINT ["/manager"]
