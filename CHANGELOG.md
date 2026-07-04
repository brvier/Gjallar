# Changelog

All notable changes to Gjallar are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

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
