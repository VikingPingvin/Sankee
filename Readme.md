# Sankee — Sankey Expense Tracker

A single-binary expense visualiser that renders your budget as an interactive Sankey diagram.
Built with Go + HTMX + D3. No installation required beyond dropping the binary.

---

## Features

- Interactive Sankey diagram driven by D3
- Multiple isolated workspaces per instance
- Persistent storage via SQLite (default) or PostgreSQL
- Single binary — no runtime dependencies, no Docker required
- Fully configurable via config file, environment variables, or CLI flags

---

## Quick start

### Download a release binary

Grab the latest binary for your platform from the [Releases](../../releases) page:

```sh
# Linux (amd64)
curl -L https://github.com/VikingPingvin/sankee/releases/latest/download/sankee-linux-amd64 -o sankee
chmod +x sankee
./sankee
```

Then open http://localhost:8080.

### Run with Docker

```sh
docker run -p 8080:8080 -v sankee_data:/data \
  -e SANKEE_DB_DSN=/data/sankee.db \
  ghcr.io/vikingpingvin/sankee:latest
```

Or with Compose:

```sh
docker compose up
```

The SQLite database is stored in a named volume (`sankee_data`) so it survives container restarts.

### Build from source

Requires Go 1.21+.

```sh
git clone https://github.com/VikingPingvin/sankee
cd sankee
go build -o sankee .
./sankee
```

---

## Configuration

Configuration is loaded in precedence order:
**CLI flags** > **environment variables** > **config file** > **built-in defaults**

### Config file (`config.json`)

Copy the sample and edit as needed:

```sh
cp config.json.sample config.json
```

| Field            | Default          | Description                                          |
|------------------|------------------|------------------------------------------------------|
| `addr`           | `localhost:8080` | Listen address                                       |
| `debug_populate` | `false`          | Seed the first workspace with sample data on startup |
| `db_driver`      | `sqlite`         | Database driver: `sqlite` or `postgres`              |
| `db_dsn`         | `sankee.db`      | SQLite file path or PostgreSQL connection string      |

### Environment variables

All variables are prefixed with `SANKEE_`:

| Variable               | Equivalent field   |
|------------------------|--------------------|
| `SANKEE_ADDR`          | `addr`             |
| `SANKEE_DEBUG_POPULATE`| `debug_populate`   |
| `SANKEE_DB_DRIVER`     | `db_driver`        |
| `SANKEE_DB_DSN`        | `db_dsn`           |

### CLI flags

```
  -addr string
        listen address (overrides config and env)
  -config string
        path to JSON config file (default "config.json")
  -db_driver string
        database driver: sqlite or postgres
  -db_dsn string
        database DSN: file path for sqlite, connection string for postgres
  -debug_populate
        seed first workspace with sample data on startup
  -version
        print version and exit
```

---

## Database

### SQLite (default)

No setup needed. The database file is created automatically at the path set by `db_dsn`.

```json
{ "db_driver": "sqlite", "db_dsn": "sankee.db" }
```

WAL mode is enabled automatically for better concurrent read performance.

### PostgreSQL

```sh
SANKEE_DB_DRIVER=postgres \
SANKEE_DB_DSN="postgres://user:pass@host:5432/sankee?sslmode=require" \
./sankee
```

The schema is created automatically on first run.

---

## Development

```sh
# Run dev server
go run .

# Run tests
go test ./...

# Cross-compile release builds (requires task)
task dist
```

---

## License

MIT
