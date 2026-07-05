# Gjallar

A KISS monitoring service: **one static binary, one YAML config file, one SQLite file.**

Gjallar watches your services, shows a status page with history (black & red,
HTMX-refreshed), and alerts you when something goes down — and when it recovers.

![status page](logo.png)

## Monitor types

| Type | What it checks |
|---|---|
| `http` | HTTP/HTTPS request: status code (`expect_status`), body regex, TLS certificate expiry (`cert_expiry_warn`) |
| `postgres` | SQL query result against a rule (pure-Go pgx driver) |
| `oracle` | SQL query result against a rule (pure-Go go-ora driver, no Oracle client needed) |
| `ping` | ICMP echo (privileged or unprivileged) |
| `prometheus` | Fetches a `/metrics` route and evaluates a rule against metric values, with optional label selectors |

Rules: `> N`, `>= N`, `< N`, `<= N`, `== x`, `!= x`, `~ regex`, `rows > 0` (row count).

## Alerting

- Any [shoutrrr](https://shoutrrr.nickfedor.com/) URL: Telegram, email/SMTP, ntfy,
  Discord, Slack, Gotify, Pushover, generic webhooks, ...
- Free Mobile SMS API (`type: freemobile`).
- Signal via a [signal-cli-rest-api](https://github.com/bbernhard/signal-cli-rest-api)
  gateway (`type: signal`: `url` to `/v2/send`, sender `number`, `recipients`).

A monitor alerts after `failure_threshold` consecutive failures (no flapping noise),
and again on recovery. Open incidents survive restarts: no duplicate alerts.

## Quick start

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"
cp gjallar.example.yaml gjallar.yaml   # edit to taste
./gjallar -config gjallar.yaml
```

Then open <http://localhost:8080>. The page auto-refreshes every 10 s.

Configuration reference: see [`gjallar.example.yaml`](gjallar.example.yaml) —
every field is documented there.

### Secrets

Any config value may reference an environment variable with `${VAR}` syntax:

```yaml
dsn: "postgres://monitor:${PG_PASSWORD}@db1:5432/app"
```

Startup fails with a clear error if a referenced variable is undefined. A bare
`$` (e.g. in a `~ ^OPEN$` regex rule) is left untouched.

### Reload

Gjallar reloads its configuration on `SIGHUP` (`systemctl reload gjallar`).
The new config is fully validated first — if it is broken, the running
configuration is kept and the error is logged.

### Groups

Give monitors an optional `group: "Hyperion"` and the status page shows them
under a common header with an `up/total` count. Monitors without a group are
listed first, without a header. Groups appear in config order.

### Re-alerts

By default a monitor alerts once when it goes down and once when it recovers.
Set `realert: 1h` (globally in `defaults` or per monitor) to also get a
"still down" reminder at that interval during long outages.

## Ping permissions

Unprivileged ping (`privileged: false`, the default) needs:

```sh
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
```

Privileged ping (`privileged: true`) needs raw sockets:

```sh
sudo setcap cap_net_raw+ep ./gjallar
```

or the systemd unit's `AmbientCapabilities=CAP_NET_RAW` (see
[`deploy/gjallar.service`](deploy/gjallar.service)). Gjallar self-tests ping at
startup and exits with the fix if permissions are missing.

## Storage & retention

Check results and incidents live in a single SQLite file (`database`). Results
older than `retention` (default 30 days) are pruned hourly. Back up by copying
the file.

## Tests

```sh
go test ./...
```

Database integration tests are skipped unless pointed at a server:

```sh
docker run --rm -e POSTGRES_PASSWORD=secret -p 5432:5432 postgres:16
GJALLAR_TEST_PG_DSN="postgres://postgres:secret@localhost:5432/postgres?sslmode=disable" go test ./internal/check/

docker run --rm -p 1521:1521 -e ORACLE_PASSWORD=secret gvenzl/oracle-free:23-slim
GJALLAR_TEST_ORA_DSN="oracle://system:secret@localhost:1521/FREEPDB1" go test ./internal/check/
```

## Design

- One goroutine per monitor → one results channel → one consumer
  (single SQLite writer, lock-free alert state machine).
- Everything embedded: templates, htmx, CSS, logo. No CDN, no CGO, no ORM,
  no router dependency.
