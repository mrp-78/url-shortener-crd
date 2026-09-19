FROM golang:alpine AS builder
WORKDIR /app
ENV GOPROXY="https://goproxy.io,direct"
ENV GOSUMDB=off
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o url-shortener-backend .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /app/url-shortener-backend /usr/local/bin/
RUN mkdir -p /data
EXPOSE 8080
ENV DB_PATH=/data/urls.db
ENV PORT=8080
ENV BASE_URL=http://localhost:8080
ENTRYPOINT ["/usr/local/bin/url-shortener-backend"]
