# Go Glyph REST API

A REST API for retrieving Dota 2 glyph information based on match ID.

Built with Fiber, PostgreSQL, [go-dota2](https://github.com/paralin/go-dota2)
and [Dotabuff manta parser](https://github.com/dotabuff/manta).

**Input:** Match ID  
**Output:** Glyph information

See swagger page for more information.

## Prerequisites

* Golang 1.24 or higher
* Have a PostgreSQL database running
* Steam account with Dota 2
* Create `.env` file in the root directory with:

```
# Database settings:
POSTGRES_HOST="hostname"
POSTGRES_USER="postgres"
POSTGRES_PASSWORD="postgres"
POSTGRES_DB="postgres"
POSTGRES_PORT=5432
SSL_MODE="disable"

# Steam settings:
STEAM_LOGIN_USERNAMES="your_steam_login"
STEAM_LOGIN_PASSWORDS="your_steam_password"
```

## Running the Application

To run the application with protobuf conflict warnings instead of panics:

### Using Shell Script

```bash
# Make the script executable
chmod +x run.sh

# Run the application
./run.sh
```

### Manual Build & Run

```bash
# Build with conflict warnings enabled
go build -ldflags "-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn"

# Run the built binary
./go-glyph

# Or run directly with environment variable
GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn go run main.go
```

## Credits

Original author: [Masedko](https://github.com/Masedko/glyph)
