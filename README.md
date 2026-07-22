<div align="center">

# Shelly Energy Dashboard

### Local-first energy intelligence for your Shelly devices

Discover devices, monitor whole-house power, understand room-level usage, and estimate energy costs — without sending household data to the cloud.

[![CI](https://github.com/filipegalo/shelly-energy-dashboard/actions/workflows/ci.yml/badge.svg)](https://github.com/filipegalo/shelly-energy-dashboard/actions/workflows/ci.yml)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Container](https://img.shields.io/badge/Container-ghcr.io-2496ED?logo=docker&logoColor=white)](https://github.com/filipegalo/shelly-energy-dashboard/pkgs/container/shelly-energy-dashboard)
[![Local first](https://img.shields.io/badge/data-local--first-7C3AED)](#privacy-by-design)

[Quick start](#quick-start) · [Documentation](docs/README.md) · [Configuration](docs/configuration.md) · [Development](docs/development.md)

</div>

---

Shelly Energy Dashboard is a self-hosted dashboard for Shelly smart-home devices. A single Go service discovers devices on networks you explicitly allow, stores inventory and energy history in SQLite, and streams live updates to a responsive web interface.

No Shelly Cloud. No browser-to-device requests. No external CDN dependencies.

> [!IMPORTANT]
> The application currently has no user accounts. Keep it on a trusted management network or place it behind an authenticating TLS reverse proxy.

## What it does

| | Capability | Highlights |
|---|---|---|
| 🔎 | **Device discovery** | Bounded CIDR scans, optional mDNS candidates, Gen1 and Gen2/3 support |
| ⚡ | **Live energy** | Whole-house power, durable history, billing cycles, meter health |
| 🏠 | **Room insights** | Per-channel room attribution, submeters, reconciliation, cost allocation |
| 💶 | **Cost estimates** | Effective-dated decimal pricing, VAT tiers, excise tax, Portugal/EUR defaults |
| 🔐 | **Local security** | Encrypted credentials, SSRF-resistant discovery, CSRF and security headers |
| 📈 | **Operations** | Health checks, readiness, Prometheus metrics, backups, deterministic mocks |

## Quick start

```bash
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker compose up -d --build
```

Keep the encryption key: saved credential profiles cannot be decrypted without it.

Open [http://localhost:8080](http://localhost:8080), add a CIDR in **Networks** — for example `192.168.20.0/24` — and start a scan.

Data is persisted in the `shelly-data` Docker volume. Compose publishes the service on host loopback by default.

Stop with `docker compose down`. Do not add `-v` unless you intend to delete the persistent data volume.

For the published multi-architecture image instead of a local build:

```bash
docker pull ghcr.io/filipegalo/shelly-energy-dashboard:latest
```

See the [complete setup guide](docs/getting-started.md) for direct `docker run`, first meter configuration, release tags, and shutdown guidance.

## How it fits together

```text
Browser ── HTTP / HTMX / SSE ──> Go service
                                  ├── discovery + Shelly local API client
                                  ├── scheduler + shared energy collector
                                  ├── pricing + room reconciliation
                                  ├── embedded UI assets
                                  └── SQLite (WAL)
                                           │
                                           └── backend-only HTTP to Shelly VLANs
```

The production image is self-contained: templates, Tailwind CSS, HTMX, Chart.js, migrations, and JavaScript are embedded at build time. It contains no Node.js runtime, Go compiler, or source tree.

Read the [architecture guide](docs/architecture.md) for component boundaries, discovery rules, identity reconciliation, and energy data behavior.

## Privacy by design

- Devices are contacted only on explicitly configured networks.
- The browser never connects directly to a Shelly device.
- Redirects and environment HTTP proxies are disabled for device requests.
- Saved passwords are encrypted with AES-256-GCM.
- Device responses, credentials, cookies, and authorization headers are excluded from logs.
- Charts and application assets are bundled locally.

The [security guide](docs/security.md) covers the trust model, network exposure, credential handling, and deployment checklist.

## Documentation

| Guide | Use it for |
|---|---|
| [Getting started](docs/getting-started.md) | Installation, first scan, first energy meter, container images |
| [Configuration](docs/configuration.md) | Environment variables, VLAN routing, mDNS, Shelly authentication |
| [Energy monitoring](docs/energy-monitoring.md) | Collection, pricing, retention, room metering, reconciliation |
| [Architecture](docs/architecture.md) | Runtime design, discovery safety, identity, APIs, repository map |
| [Operations](docs/operations.md) | Health, metrics, backup and restore, production practices |
| [Development](docs/development.md) | Local toolchain, Make targets, mock devices and test fixtures |
| [Security](docs/security.md) | Threat model, controls, reverse-proxy requirements |
| [Troubleshooting](docs/troubleshooting.md) | Common failures, diagnostics, current limitations |

## Local development

Requires Go 1.24+ and Node.js 22+.

```bash
npm ci
npm run assets
go test -race -cover ./...
go run ./cmd/server
```

`APP_ENCRYPTION_KEY` is required before saving device credentials. See the [development guide](docs/development.md) for the full build workflow and deterministic Shelly mocks.

## Scope

This project monitors Shelly devices; it intentionally does not control switches, lights, covers, gates, firmware, or device configuration. It is IPv4-only today and supports one whole-house meter per deployment.

More limitations and likely extensions are tracked in [Troubleshooting → Current limitations](docs/troubleshooting.md#current-limitations).

---

<div align="center">

Built for private, understandable, self-hosted energy monitoring.

</div>
