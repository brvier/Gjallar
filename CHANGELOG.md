# Changelog

All notable changes to Gjallar are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.8.1] - 2026-07-06

### Changed

- Clearer disabled-monitor UX: a distinct dashed DISABLED badge, a dashed card
  outline on the status page, and an explicit banner on the monitor detail page.

## [0.8.0] - 2026-07-06

### Added

- `enabled: false` on a monitor: keep it on the status page (shown as a grey
  DISABLED badge, excluded from the up/total count) without running any check
  or alert. Defaults to enabled.

## [0.7.0] - 2026-07-06

### Added

- Elasticsearch check type (`type: elasticsearch`): index freshness measured
  as hours since `max(timestamp_field)`, evaluated against a rule (e.g. "< 3").
  Fields: `url`, `index`, `timestamp_field`, `rule`.

## [0.6.0] - 2026-07-06

### Added

- `-check` flag: validate the configuration (monitors, rules, alert URLs)
  and exit without running any check or sending any alert.

## [0.5.0] - 2026-07-05

### Added

- Redis check type (`type: redis`): TCP connect, optional `AUTH`, `PING`
  must answer `+PONG`. Fields: `host`, `port` (default 6379), `password`.

## [0.4.0] - 2026-07-05

### Added

- Signal alerts (`type: signal`) through any signal-cli-rest-api compatible
  gateway: POST to `/v2/send` with sender `number` and `recipients`.

## [0.3.0] - 2026-07-04

### Added

- Monitor groups: an optional `group` field on monitors; the status page
  shows grouped monitors under a header with an up/total count.

## [0.2.0] - 2026-07-04

### Added

- `${VAR}` environment variable expansion in the config file, for secrets.
  Startup fails if a referenced variable is undefined.
- `realert` option (defaults or per monitor): "still down" reminder alerts at
  the given interval during long outages.
- Configuration reload on SIGHUP (`systemctl reload gjallar`). A broken new
  config is rejected and the running configuration is kept.
- GitHub Actions CI (vet, test, build).

### Changed

- The embedded logo is now 128 px (~14 KB instead of ~1 MB), shrinking the
  binary and the page weight.

## [0.1.0] - 2026-07-04

### Added

- Monitors: HTTP/HTTPS (status code, body regex, TLS certificate expiry),
  PostgreSQL, Oracle, ICMP ping, Prometheus metrics.
- Rule language: `> N`, `>= N`, `< N`, `<= N`, `== x`, `!= x`, `~ regex`, `rows > 0`.
- Status page (html/template + HTMX, black & red theme) with uptime bars,
  latency sparkline and incident history.
- SQLite storage with configurable retention.
- Alerting via shoutrrr (Telegram, SMTP, ntfy, Discord, Slack, webhooks, ...)
  and the Free Mobile SMS API, with failure thresholds and recovery notifications.
- Single static CGO-free binary; YAML configuration; systemd unit example.
