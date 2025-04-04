FROM golang:1.21-alpine

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the DVM server
RUN go build -o dvm ./cmd/dvm && \
    chmod 755 dvm && \
    ls -la dvm

# Switch to an explicit shell to run the command
CMD ["/bin/sh", "-c", "./dvm"]