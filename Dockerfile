# consumer/Dockerfile

FROM golang:1.24.3-alpine as builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o vpnconsumer ./cmd/vpnconsumer

# Final stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/vpnconsumer .
COPY --from=builder /app/.env .env

RUN apk add --no-cache ca-certificates

CMD ["./vpnconsumer"]
