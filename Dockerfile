FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build both binaries
RUN go build -o gateway-bin ./gateway/main.go
RUN go build -o processor-bin ./processor/main.go

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/gateway-bin .
COPY --from=builder /app/processor-bin .
CMD ["./gateway-bin"]

