# consumer/Dockerfile

FROM golang:1.24.6-alpine3.21 as builder

WORKDIR /app

RUN apk add --no-cache git curl

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o vpnconsumer ./cmd/vpnconsumer

# Final stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/vpnconsumer .

RUN apk add --no-cache ca-certificates

CMD ["./vpnconsumer"]
