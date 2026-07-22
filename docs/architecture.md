# Architecture

Shelly Energy Dashboard is one Go process with an embedded web interface, durable SQLite state, and backend-only communication with Shelly devices.

```text
Browser ──HTTP/HTMX/SSE──> one Go process
                              ├── html/template + embedded static assets
                              ├── chi routes and security middleware
                              ├── bounded CIDR scanner + mDNS candidates
                              ├── Shelly Gen1 and Gen2/3 RPC client
                              ├── in-process scheduler and scan registry
                              ├── shared multi-source energy collector
                              ├── room attribution + reconciliation
                              ├── decimal pricing + billing-cycle engine
                              ├── SQLite (WAL) repositories
                              └── health, readiness, Prometheus metrics
                                      │
                                      └── backend-only HTTP to Shelly VLANs
```

Templates, compiled Tailwind CSS, HTMX, Chart.js, migrations, and application JavaScript are embedded at build time. The production image has no Node.js runtime, Go compiler, or source tree.

## Discovery behavior

Active discovery enumerates usable IPv4 addresses in a configured CIDR, skips traditional network and broadcast addresses, and feeds a fixed-size worker pool. It does not require an ICMP response. Each worker requests only fixed local paths: `/shelly`, then Gen1 `/settings` and `/status`, or Gen2/newer RPC status and configuration methods. Unsupported optional RPC methods are nonfatal.

mDNS is a best-effort candidate source. Every advertised address is checked against the selected configured network before use; active CIDR scanning remains authoritative.

Manual discovery accepts only a literal IP or plain hostname—never a URL, port, userinfo, or path. Resolution produces literal addresses, and the enabled-network policy is checked again before the backend connects.

## Stable device identity

Devices reconcile in this order:

1. Shelly device ID
2. Normalized MAC address
3. IP address, but only for a provisional identity

An IP change updates the existing record while preserving custom name, room, credential profile, and first-seen time. Completed scans increment missed-device failures; cancelled or failed scans do not. Scanning never deletes inventory.

## Energy data flow

The shared collector polls the main meter and active room sources without depending on a browser session. It refreshes the current IP from stable inventory identity, reuses the encrypted credential profile, bounds concurrency, and deduplicates a component used more than once.

Raw readings feed minute, hour, and day aggregates. Room history reuses those source samples and aggregates instead of maintaining a second room-specific reading store. Effective-dated assignments decide which room owns a sample at reporting time.

See [Energy monitoring](energy-monitoring.md) for counter resets, missing samples, pricing, and reconciliation semantics.

## HTTP surfaces

- The HTML interface uses server-rendered templates and HTMX.
- Server-Sent Events stream live scan and energy state.
- `GET /energy/events` exposes live whole-house energy state.
- The browser reads energy history from `GET /energy/history?range=24h|7d|30d|billing|previous_billing|month`.
- Custom history uses `range=custom&start=YYYY-MM-DD&end=YYYY-MM-DD` and is capped at 366 days.
- `GET /health`, `GET /ready`, and `GET /metrics` support operations.

The energy JSON routes are internal browser endpoints, not a versioned public API. They may change with the web interface.

## Naming note

The product, repository, and published image use `shelly-energy-dashboard`. Some internal names retain the project's earlier inventory focus: the Compose service and binary are `shelly-inventory`, and the default database file is `shelly-inventory.db`. These names are expected and do not indicate a different application.

## Repository map

```text
cmd/server/             composition root and graceful HTTP server
cmd/mock-shelly/        development-only mock device
internal/config/        environment parsing and validation
internal/credentials/   AES-256-GCM password encryption
internal/database/      SQLite schema and repositories
internal/energy/        collection, aggregation, attribution, billing, pricing
internal/devices/       stable identity and MAC normalization
internal/networks/      CIDR policy and safe host enumeration
internal/shelly/        client, detection, Gen1/RPC normalization, Digest auth
internal/scans/         bounded runner, cancellation, SSE snapshots, mDNS
internal/scheduler/     recurring scan coordinator
internal/metrics/       bounded-label Prometheus collectors
internal/web/           handlers, middleware, templates, embedded assets
```

---

[Documentation index](README.md) · [Energy monitoring](energy-monitoring.md) · [Development](development.md)
