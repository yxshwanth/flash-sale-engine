FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build both binaries (build entire packages, not single files)
RUN go build -o gateway-bin ./gateway
RUN go build -o processor-bin ./processor

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/gateway-bin .
COPY --from=builder /app/processor-bin .
CMD ["./gateway-bin"]

