# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Sifa is a monitoring and alerting service written in Go that tracks "target" activity and sends email notifications when targets haven't been acted upon within specified time windows. It combines a REST API server with a cron-based scheduler in a multi-threaded architecture.

## Architecture

### Multi-Threaded Design

The application runs two concurrent long-running processes coordinated via context cancellation:

1. **HTTP Server** (`runServer`): Chi-based REST API that receives POST requests when targets act
2. **Alert Scheduler** (`runScheduler`): Cron job runner that checks target activity and sends notifications

Both are launched in goroutines from `main()` and coordinate via a `context.Context` for graceful shutdown on SIGINT/SIGTERM.

### Data Flow

1. External systems POST to `/{id}` endpoint to signal a target has acted
2. Handler (`TargetActed`) validates target ID exists in config and stores current timestamp in BadgerDB
3. Scheduler runs on cron schedule (currently `* * * * *` for testing, intended for `@hourly`)
4. For each target, scheduler:
   - Reads last acting timestamp from BadgerDB
   - Compares against `maxAge` threshold (in seconds)
   - If threshold exceeded, sends email via Postmark API
   - Checks if alert should be sent based on `alertSchedule` cron expression

### Storage

Uses BadgerDB (embedded key-value store) in `./db/` directory:
- Key: target ID (string)
- Value: marshaled `time.Time` of last activity

### Authentication

All HTTP endpoints (except `/health`) protected by `WithToken` middleware that validates `x-api-token` header against `TOKEN` environment variable.

## Configuration

### config.json

Defines monitored targets:

```json
{
  "targets": [
    {
      "id": "arusa",           // Unique target identifier
      "maxAge": 10,            // Max seconds without activity before alert
      "alertSchedule": "0 9 * * *"  // Cron expression for alert timing
    }
  ]
}
```

### Environment Variables (.env)

- `POSTMARK_TOKEN`: API token for Postmark email service
- `TOKEN`: API authentication token for incoming requests
- `PORT`: HTTP server port (default: 3010)

## Development Commands

### Build and Run

```bash
# Build the application
go build -o sifa .

# Run locally
./sifa

# Run with auto-reload during development
go run .
```

### Testing

```bash
# Test the health endpoint
curl http://localhost:3010/health

# Test target acting (requires valid TOKEN)
curl -X POST http://localhost:3010/arusa \
  -H "x-api-token: your-token-here"
```

### Dependencies

```bash
# Download dependencies
go mod download

# Update dependencies
go mod tidy
```

### Docker

```bash
# Build Docker image
docker build -t sifa .

# Run container (requires .env and config.json)
docker run --env-file .env -v $(pwd)/config.json:/app/config.json -p 3010:3010 sifa
```

Note: The Dockerfile creates a multi-stage build with a minimal Alpine-based image running as non-root user.

## Key Dependencies

- `github.com/go-chi/chi/v5`: HTTP router and middleware
- `github.com/dgraph-io/badger/v4`: Embedded database for persistence
- `github.com/adhocore/gronx`: Cron expression parser and task scheduler
- `github.com/joho/godotenv`: Environment variable loading

## Important Implementation Notes

### Email Delivery

Email notifications are sent via Postmark API (`mail.go`). Currently hardcoded recipient address at `server.go:130` - needs to be made configurable per target.

### Scheduler Configuration

Alert task currently runs every minute (`* * * * *` at `server.go:99`) for testing. Should be changed to `@hourly` or appropriate interval for production.

### Error Handling

When a target hasn't acted yet, the database read returns an error (expected behavior). The scheduler logs this and continues with next target.

### BadgerDB Transactions

All database operations use Badger transactions:
- Reads: `db.View()`
- Writes: `db.Update()`
- Time values marshaled/unmarshaled with `time.Time.MarshalBinary()`
