# ---------- Stage 1: Build ----------
FROM golang:1.25-alpine AS builder

# Set the working directory
WORKDIR /app

# Use Docker's automatic platform detection
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

# Ensure a portable, static-ish binary
ENV CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v}

# Copy and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the Go application (strip debug info for smaller size)
RUN go build -trimpath -ldflags="-s -w" -o sifa .

# ---------- Stage 2: Final ----------
FROM alpine:latest

# Set the working directory
WORKDIR /app

# Install runtime dependencies you actually need
# RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN addgroup -S appuser \
  && adduser -S -G appuser -H -s /sbin/nologin appuser

# Copy the binary and set ownership
COPY --from=builder --chown=appuser:appuser /app/sifa /app/sifa

# Create db directory with proper permissions
RUN mkdir -p /app/db && chown -R appuser:appuser /app/db

# Run as non-root user
USER appuser

# Set the entrypoint command
ENTRYPOINT ["/app/sifa"]
