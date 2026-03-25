FROM golang:1.25.1-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o oclird ./cmd/oclird

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/oclird /app/oclird
EXPOSE 8765
ENTRYPOINT ["/app/oclird"]
