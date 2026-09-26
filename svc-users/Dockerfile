# STAGE 1: Build the binary
FROM golang:1.26.5-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the application pointing specifically to the cmd folder
RUN CGO_ENABLED=0 GOOS=linux go build -o users-service ./cmd

# STAGE 2: Create the final production image
FROM alpine:latest

# Install CA certificates for secure HTTPS/TLS connections (like MongoDB Atlas)
RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/users-service .

EXPOSE 8001
CMD ["./users-service"]
