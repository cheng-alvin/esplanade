FROM golang:1.23-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    	go build -ldflags="-s -w" -o /esplanade-server .

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app

COPY --from=builder /esplanade-server /usr/local/bin/esplanade-server

USER app
EXPOSE 8080
ENTRYPOINT ["esplanade-server"]
