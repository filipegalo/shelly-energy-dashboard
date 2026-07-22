# Shelly Energy Dashboard

Shelly Energy Dashboard is a self-hosted, local-only inventory, whole-house energy monitor, and per-room energy dashboard for Shelly smart-home devices. One Go process serves the responsive web interface, discovers devices on explicitly configured networks, queries their local APIs, stores normalized inventory and energy history in SQLite, schedules recurring work, and streams live state to the browser with Server-Sent Events. It has no Shelly Cloud dependency and never asks the browser to contact a device.

> This MVP has no user accounts. Deploy it on a trusted management network or behind an authenticating TLS reverse proxy.

## Interface overview

The dashboard summarizes online, offline, authentication-required, and unassigned devices. The Devices view supports search and filters; Rooms organize inventory without coupling identity to DHCP addresses; Networks defines the only CIDRs the scanner may contact; Scan history shows durable progress and errors. Energy selects one discovered Shelly meter as the whole-house source, then shows live power, durable history, billing-cycle summaries, and Portugal/EUR variable-cost estimates. Energy → Rooms attributes independently addressable meter channels to rooms, compares their consumption and allocated cost, and reconciles them against the main meter. Device details include normalized identity, network and firmware data, capabilities, credentials, diagnostics, and collapsible raw JSON.

## Architecture

```text
Browser ──HTTP/HTMX/SSE──> one Go process
                              ├── html/template + embedded static assets
                              ├── chi routes and security middleware
                              ├── bounded CIDR scanner + mDNS candidates
                              ├── Shelly Gen1 and Gen2/3 RPC client
                              ├── in-process scheduler and scan registry
                              ├── shared multi-source energy collector + aggregation
                              ├── effective-dated room attribution + reconciliation
                              ├── decimal pricing and billing-cycle engine
                              ├── SQLite (WAL) repositories
                              └── health, readiness, Prometheus metrics
                                      │
                                      └── backend-only HTTP to Shelly VLANs
```

Templates, compiled Tailwind CSS, HTMX, migrations, and static JavaScript are embedded at build time. The production image contains no Node.js runtime, Go compiler, or source tree.

## Repository structure

```text
cmd/server/             composition root and graceful HTTP server
cmd/mock-shelly/        development-only mock device
internal/config/        environment parsing and validation
internal/credentials/   AES-256-GCM password encryption
internal/database/      SQLite schema and repositories
internal/energy/        collection, aggregation, room attribution, reconciliation, billing, pricing
internal/devices/       stable identity and MAC normalization
internal/networks/      CIDR policy and safe host enumeration
internal/shelly/        HTTP client, detection, Gen1/RPC normalization, Digest auth
internal/scans/         bounded scan runner, cancellation, SSE snapshot hub, mDNS
internal/scheduler/     recurring scan coordinator
internal/metrics/       bounded-label Prometheus collectors
internal/web/           handlers, middleware, templates, and embedded assets
```

## Quick start with Docker Compose

Create a strong 32-byte encryption key and start the application:

```bash
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker compose up -d --build
docker compose logs -f shelly-inventory
```

Open <http://localhost:8080>. Compose publishes on host loopback by default. Data is stored in the `shelly-data` volume under `/data/shelly-inventory.db` in the container.

Add a network from **Networks**, for example `192.168.20.0/24`, keep **Enable scanning** selected, and choose **Scan**. The live view continues from the latest durable snapshot after a browser reconnect. Recurring scans run inside the Go process and do not require an open browser.

To enable energy monitoring, scan the network first, open **Energy → Settings**, select a device that exposes power-meter capability, discover and test its meter component, then save it. Collection runs in the server process and continues with no browser open. After the main meter is configured, open **Energy → Room metering** to test and assign individual components or channels to rooms.

Stop the deployment with:

```bash
docker compose down
```

Do not add `-v` unless you intentionally want Docker to remove the persistent data volume.

## Prebuilt container image

Every push to `master` publishes a multi-architecture image with an SBOM to GitHub Container Registry for `linux/amd64` and `linux/arm64`. Because this repository is public, the workflow also attaches signed build provenance:

```bash
docker pull ghcr.io/filipegalo/shelly-energy-dashboard:latest
```

Run it directly with a persistent data volume and loopback-only port binding:

```bash
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker run -d \
  --name shelly-energy-dashboard \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -e APP_ENCRYPTION_KEY \
  -e APP_LISTEN_ADDRESS=0.0.0.0:8080 \
  -e APP_DATABASE_PATH=/data/shelly-inventory.db \
  -v shelly-data:/data \
  ghcr.io/filipegalo/shelly-energy-dashboard:latest
```

The publisher also creates commit-addressed `sha-<commit>` tags. Pushing a stable release tag such as `v1.2.3` additionally publishes `1.2.3`, `1.2`, and `1`; prerelease tags such as `v1.2.3-rc.1` do not replace `latest`. Use the published image digest when strict immutability is required. The first successful publication creates the package under the repository owner's **Packages**; choose its public or private visibility in the package settings. Pulling a private package requires `docker login ghcr.io` with a GitHub token that can read packages.

## Local development

Requirements: Go 1.24+, Node.js 22+ for asset compilation, and SQLite CLI only for manual backup commands.

```bash
npm ci
npm run assets
go test -race -cover ./...
go vet ./...
go build -trimpath -o bin/shelly-inventory ./cmd/server
mkdir -p data
APP_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
APP_DATABASE_PATH=./data/shelly-inventory.db \
./bin/shelly-inventory
```

Equivalent Make targets are available:

```bash
make assets
make fmt
make test
make vet
make build
make run
make docker-build
make compose-up
```

## Configuration

| Variable | Default | Purpose |
|---|---:|---|
| `APP_LISTEN_ADDRESS` | `127.0.0.1:8080` | HTTP bind address; Compose overrides this inside the container |
| `APP_DATABASE_PATH` | `./data/shelly-inventory.db` | SQLite file |
| `APP_ENCRYPTION_KEY` | empty | Required before saving credentials; raw or base64 32 bytes |
| `APP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `APP_ENABLE_MDNS` | `false` | Browse `_shelly._tcp` and `_http._tcp`; candidates remain CIDR-restricted |
| `APP_ALLOW_NON_PRIVATE_NETWORKS` | `false` | Advanced opt-in for explicitly configured non-RFC1918 CIDRs |
| `APP_MAX_SUBNET_PREFIX` | `16` | Largest allowed IPv4 range; prefix must be `/16` or narrower |
| `APP_MAX_HOSTS` | `65534` | Absolute usable-address cap |
| `APP_DEFAULT_SCAN_CONCURRENCY` | `32` | New-network worker default |
| `APP_DEFAULT_CONNECTION_TIMEOUT` | `750ms` | TCP connection default |
| `APP_DEFAULT_REQUEST_TIMEOUT` | `2s` | Whole Shelly request default |
| `APP_OFFLINE_FAILURE_THRESHOLD` | `3` | Completed missed scans before offline |
| `APP_MAX_CONCURRENT_SCANS` | `4` | Process-wide concurrent scan cap across networks |
| `APP_ALLOWED_HOSTS` | `localhost,127.0.0.1,[::1]` | Comma-separated accepted HTTP Host names/IPs; use `*` only behind a trusted proxy |
| `APP_SECURE_COOKIES` | `false` | Force the CSRF cookie's Secure flag when TLS terminates at a reverse proxy |
| `APP_PUBLISHED_ADDRESS` | `127.0.0.1` | Compose-only host address used to publish port 8080 |

Configuration values saved on an individual Network override the timeout, interval, and concurrency defaults.

## Whole-house energy monitoring

One stable inventory device ID and one validated component key are configured as the main meter. Supported sources include Gen1 `meter`/`emeter` data and Gen2-or-newer `switch`, `EM`, `EM1`, `PM`, and data companions. Component keys map to fixed allowlisted Shelly paths; form input never selects an arbitrary URL or RPC method. A meter must pass a live test before it can be saved.

The collector polls independently of the browser, refreshes the device's current IP from its stable database identity before each request, reuses its encrypted credential profile, and applies bounded retry backoff. Cumulative imported-energy counters are preferred. If only power is available, consumption is integrated between samples; long gaps are marked partial rather than invented. Counter decreases create a reset event and never produce negative consumption. Exported totals are retained when available but excluded from household-consumption cost.

Raw readings, minute/hour/day aggregates, and meter health events are durable. Default retention is 7 days for raw samples, 90 days for minute aggregates, and 730 days for hourly aggregates; daily history is retained. Cleanup never removes the current billing cycle. Disabling monitoring pauses new collection without deleting settings or history. Changing the selected meter starts a new source series and does not merge counters.

Pricing uses decimal arithmetic and versioned effective timestamps. Defaults target Portugal Continental and EUR: €0.137378/kWh before VAT, a 200 kWh/30-day reduced-VAT allowance (300 kWh for large families), 6% and 23% VAT rates, and €0.001/kWh electricity excise tax plus configured VAT. The allowance is prorated by exact billing-cycle length. While minute aggregates are retained, a mid-hour/day pricing change uses the actual energy measured on each side of its effective minute. For older coarse history whose minute detail has expired, energy inside a bucket is allocated between price versions in proportion to elapsed time; this is an explicit approximation because the original within-bucket load shape no longer exists. Estimates include only variable imported-energy charges, VAT, and optional excise tax; they exclude fixed contracted-power and other invoice charges. Contracted kVA is recorded for audit/context but is not charged by this estimate. Edit these defaults to match the current invoice before relying on the result.

The history API is `GET /energy/history?range=24h|7d|30d|billing|previous_billing|month`; custom ranges accept `range=custom&start=YYYY-MM-DD&end=YYYY-MM-DD` and are capped at 366 days. Live state is available over `GET /energy/events`. The locally bundled Chart.js asset means chart rendering does not contact a CDN.

## Per-room energy dashboards

Room metering is configured separately from inventory room placement. An inventory room answers where the physical Shelly device is installed; an energy assignment answers which room a particular meter component measures. This distinction matters for multi-channel devices: for example, the four channels of one Shelly Pro 4PM can be assigned independently to Kitchen, Living Room, Office, and Garage even though the physical device has only one inventory location. Changing an inventory room never rewrites energy attribution.

Open **Energy → Room metering**, choose a discovered device component, test its current reading, select a room and role, and save an effective time. Component keys are validated against the server's fixed Shelly component allowlist and the device is tested by the backend before a new assignment is accepted. The main whole-house source can only be reused as a room source after an explicit warning acknowledgement.

Assignments are effective-dated. Moving a source closes its old half-open interval and creates a new assignment starting at the selected time. Earlier consumption remains with the old room. Presets cover now, the start of today, and the current billing cycle; a validated custom local date and time is also supported. Future assignments can be removed. Deleting a room with energy history archives it, closes its active assignments, and keeps it available in historical reports.

Each assignment has one aggregation role:

- **Primary** contributes power and energy to the room total. A room can have multiple primaries.
- **Submeter** explains part of a primary source and must name a primary parent in the same room. It is displayed in the source breakdown but is never added again to the room total.
- **Excluded** remains visible for diagnostics and does not affect room totals, comparison charts, reconciliation, or cost. It is polled only when diagnostic collection is enabled.

For example, a 10 kWh Kitchen circuit primary with 3 kWh Dishwasher and 2 kWh Refrigerator submeters remains a 10 kWh room total. The dashboard reports 5 kWh as other usage on that circuit. If submeters exceed their parent, it shows an overlap warning without changing the measured room total.

The shared collector polls the main meter and all active room sources without depending on an open browser. It deduplicates a component used more than once, refreshes each device by stable inventory ID so DHCP address changes are safe, respects source-specific polling intervals, bounds concurrent requests, and streams lightweight live room/source updates. Historical charts use the existing durable source samples and minute/hour/day aggregates rather than a second room-specific reading store.

### Reconciliation and allocated cost

The whole-house main meter remains the source of truth. For each aligned interval:

```text
room-metered energy = sum of root primary room sources
unmetered energy    = max(whole-house energy - room-metered energy, 0)
coverage            = room-metered energy / whole-house energy
```

**Unmetered / Other** is a calculated dashboard series, not an editable inventory room. It represents circuits without room meters, measurement gaps, standby loads, and other consumption present at the main meter. Missing source data is never converted to zero: intervals and cards are marked complete, partial, stale, missing, estimated, or reconciliation warning as appropriate.

The default over-measurement tolerance is either 5% or 0.10 kWh per interval. A small mismatch keeps measured room values and proportionally normalizes allocation. A larger mismatch is preserved and highlighted for investigation; unmetered consumption never becomes negative. Tolerances and optional diagnostic collection are configured on the room-metering page.

Per-room costs are allocated estimates. VAT and electricity pricing are calculated for the whole electricity contract and then distributed according to measured room energy consumption. The existing effective-dated Portugal/EUR pricing engine calculates each billing cycle once, including its single prorated reduced-VAT allowance and excise tax. That already-calculated variable cost is then allocated to rooms and Unmetered / Other by interval energy share. When room totals exceed the main meter, room shares are normalized against the measured-room total. A deterministic largest-remainder pass guarantees that final room costs plus unmetered cost equal the whole-house variable-cost estimate to the cent. Fixed invoice charges remain excluded, just as on the main Energy page.

Older history remains attributable after minute data expires, but a room move or price change inside an old coarse hour/day bucket may require proportional time splitting. Such points are marked estimated because the original within-bucket load shape is no longer available.

## Discovery behavior

Active discovery enumerates usable IPv4 addresses in the configured CIDR, skips traditional network/broadcast addresses, and feeds a fixed-size worker pool. It never performs an ICMP prerequisite. Each worker requests fixed local paths: `/shelly`, then Gen1 `/settings` and `/status`, or Gen2/newer RPC status/config methods. Unsupported optional RPC methods are nonfatal.

mDNS is a best-effort candidate source. Every mDNS address is checked against the selected configured network before use. Active CIDR scanning remains authoritative.

Manual discovery accepts only a literal IP or plain hostname—never a URL, port, userinfo, or path. Resolution produces literal addresses, and the selected enabled network is checked again before the backend connects.

Devices reconcile by Shelly device ID, then normalized MAC, then IP only for a provisional identity. A changed IP updates the existing record while preserving custom name, room, credential profile, and first-seen time. Completed scans increment missed-device failures; cancelled or failed scans do not. No scan deletes inventory.

## VLAN routing and firewall requirements

The container does not need to share the Shelly VLAN, but the Docker host must be able to route to it:

```text
Shelly Inventory container
        │ Docker bridge / host routing
        ▼
Docker host
        │ routed TCP/80 (or the local API port)
        ▼
IoT VLAN ──> Shelly devices
```

Allow established traffic and outbound TCP port 80 from the Docker subnet or host to the configured Shelly CIDR. Do not expose the Shelly VLAN to arbitrary internet sources. If the host itself cannot connect to `http://DEVICE_IP/shelly`, the bridge container normally cannot either. Check inter-VLAN ACLs, host routes, Docker forwarding/firewall rules, and reverse-path filtering.

Active scanning works in a normal Docker bridge when routing and firewall policy allow it. mDNS generally does **not** cross routed VLANs or Docker bridges. It may require host networking, the same Layer 2 segment, or an mDNS reflector/repeater. Keep active CIDR discovery enabled as the reliable fallback.

## Shelly authentication

Set `APP_ENCRYPTION_KEY`, then create a reusable profile in **Settings** and assign it on a device page. Passwords use AES-256-GCM with a random nonce. The browser receives only profile name and username; passwords, authorization headers, and complete authenticated requests are never logged or rendered. The local client supports Gen1 HTTP Basic and newer Shelly HTTP Digest challenges.

Do not enter credentials over untrusted plaintext HTTP. For access beyond localhost, terminate TLS at an authenticating reverse proxy, keep the Compose port bound to loopback or a proxy-only interface, set `APP_ALLOWED_HOSTS` to the public management hostname, and set `APP_SECURE_COOKIES=true`. Use a strong unique password per Shelly device; HTTP Digest responses remain susceptible to offline guessing if a stale device IP is reassigned to a malicious host, so DHCP reservations are strongly recommended for authenticated devices.

Changing or losing the key makes existing credential ciphertext undecryptable. Back up the key separately from the SQLite file. Encryption protects a copied database; it cannot protect credentials from a compromised running process.

## Mock devices

The development mock supports Gen1, Gen2, Gen3, Digest-protected, malformed, slow, unsupported-RPC, stable-identity/IP-change, deterministic three-phase energy, and per-room metering profiles. Energy variants cover normal counters, a counter reset, invalid CT state, slow responses, multi-channel meters, smart plugs, stale/offline sources, and reconciliation mismatches. Launch the app and mocks on a dedicated Docker subnet:

```bash
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker compose -f docker-compose.yml -f docker-compose.mock.yml up -d --build
```

Attach the inventory service to the mock network for an end-to-end scan:

```bash
docker network connect shelly_energy_dashboard_shelly-mocks shelly-inventory
```

Then add the RFC1918 test network `172.28.0.0/27` in the UI. For a single local mock without Docker:

```bash
MOCK_PROFILE=gen2 MOCK_LISTEN_ADDRESS=127.0.0.1:9080 go run ./cmd/mock-shelly
```

The production scanner intentionally does not accept arbitrary ports or loopback targets, so the single local command is useful for client development, not a production-policy scan.

The energy mocks use `172.28.0.11` through `172.28.0.15`. After scanning, choose the `mock-energy` device and component `em:0`. `mock-energy-reset` rolls its imported counter back after two reads, `mock-energy-invalid` reports a CT configuration error, `mock-energy-slow` exceeds normal request timeouts, and `mock-energy-flaky` transitions from online readings to a disconnected meter.

The room-energy fixtures use `172.28.0.16` through `172.28.0.29`. Every successful component read advances its cumulative counter by exactly one 10-second share of its documented daily energy, so repeated test runs are deterministic.

| Address | Profile | Intended use |
|---|---|---|
| `172.28.0.16` | `room-main3em` | Three-phase whole-house main meter at 30 kWh/day |
| `172.28.0.17` | `room-pro4pm` | Channel 0 Kitchen 10, channel 1 Living Room 5, channel 2 Office 4, channel 3 Garage 2 kWh/day |
| `172.28.0.18` | `room-em2` | Alternative two-channel meter: Kitchen 10 and Living Room 5 kWh/day |
| `172.28.0.19` | `room-plug-dishwasher` | Kitchen dishwasher submeter at 3 kWh/day |
| `172.28.0.20` | `room-plug-refrigerator` | Kitchen refrigerator submeter at 2 kWh/day |
| `172.28.0.21`–`.23` | room smart plugs | Living Room 5, Office 4, and Garage 2 kWh/day alternatives |
| `172.28.0.24` | `room-source-offline` | Goes offline after two successful energy reads |
| `172.28.0.25` | `room-source-stale` | Leaves one valid reading, then stops updating while identity remains reachable |
| `172.28.0.26` | `room-source-reset` | Resets its cumulative counter on the third energy read |
| `172.28.0.27` | `reconcile-below` | Room channels total 21 kWh/day against the 30 kWh/day main meter |
| `172.28.0.28` | `reconcile-slight-over` | Room channels total 30.6 kWh/day, a small over-measurement |
| `172.28.0.29` | `reconcile-significant-over` | Room channels total 36 kWh/day, a material over-measurement |

For the baseline reconciliation scenario, configure `.16` component `em:0` as the main meter and `.17` components `switch:0` through `switch:3` as the four room primary sources. Those rooms total 21 kWh/day, leaving `Unmetered / Other` at 9 kWh/day. Configure the dishwasher and refrigerator plugs as **submeters** beneath Kitchen `switch:0`; they explain 5 kWh/day of the 10 kWh/day Kitchen circuit but must not be added to the room total. The `.18` and `.21`–`.23` fixtures are alternative sources and should not simultaneously be configured as primaries over the same loads.

To test over-measurement, replace the `.17` channel assignments with the corresponding channels from `.28` or `.29`; do not add both sets. The mock exposes only fixed Shelly status RPC endpoints and validates component IDs, so it cannot be used to proxy arbitrary URLs or RPC methods.

## Health and metrics

- `GET /health` proves the process is serving.
- `GET /ready` pings SQLite and returns `503` if unavailable.
- `GET /metrics` exposes Prometheus text metrics.

Metrics cover scan counts/durations by terminal status, addresses scanned, discovered/new devices, local API durations/failures, online/offline/auth-required device gauges, and running scans. Energy collectors expose request count/duration/errors, latest-reading time, live watts, process-recorded kWh, reset/missing-sample counters, and the current billing-cycle variable-cost estimate. Room metrics cover selected, online, and stale sources plus aggregation, coverage, unmetered energy, reconciliation, and allocation health. Labels intentionally exclude room names, device IDs, IPs, component keys, and scan IDs to control cardinality.

## Backup and restore

For a locally stored database, SQLite's online backup command preserves a consistent WAL snapshot:

```bash
mkdir -p backups
sqlite3 ./data/shelly-inventory.db ".backup './backups/shelly-inventory-$(date +%F).db'"
```

For the Compose volume:

```bash
docker compose stop shelly-inventory
docker run --rm -v shelly_energy_dashboard_shelly-data:/data -v "$PWD/backups:/backup" alpine \
  cp /data/shelly-inventory.db /backup/shelly-inventory.db
docker compose start shelly-inventory
```

Restore only while the app is stopped:

```bash
docker compose stop shelly-inventory
docker run --rm -v shelly_energy_dashboard_shelly-data:/data -v "$PWD/backups:/backup:ro" alpine \
  cp /backup/shelly-inventory.db /data/shelly-inventory.db
docker compose start shelly-inventory
```

Keep the encryption key with the backup procedure, but not in the same unprotected location.

## Security model

- Only explicitly configured networks are scanned; public ranges are rejected by default.
- URLs and device-selected paths are not accepted. Redirects and environment HTTP proxies are disabled.
- Dialing uses the validated literal target address, closing DNS-rebinding and redirect SSRF paths.
- CIDR size, workers, TCP/request timeouts, headers, forms, and JSON bodies are bounded; Shelly response bodies are capped at 1 MiB.
- Templates escape all device strings. Raw JSON is rendered as text, never trusted HTML.
- Mutations require a SameSite Strict double-submit CSRF token; standard CSP, framing, MIME, referrer, and permission headers are set.
- SQL is parameterized; SQLite foreign keys, busy timeout, WAL, and a one-active-scan partial unique index are enabled.
- Logs are structured JSON and omit passwords, cookies, authorization headers, and response bodies.

## Troubleshooting

- **No devices found:** verify `curl http://DEVICE_IP/shelly` from the Docker host, then check CIDR, enabled state, VLAN route, ACLs, and TCP/80.
- **Scan has many timeouts:** lower concurrency on congested Wi-Fi or increase per-network connection/request timeout.
- **Device becomes offline:** the default requires three completed scans that miss it. Cancelled/failed scans do not age device status.
- **Authentication required persists:** assign the correct credential profile, then refresh the device. Shelly usernames are commonly `admin`, but local configuration is authoritative.
- **mDNS finds nothing:** expected across routed VLANs and Docker bridge networks; use CIDR scanning or deploy a reflector.
- **Database busy:** ensure only one app instance uses the database and that `/data` is persistent local storage rather than a high-latency network filesystem.
- **Energy page has no eligible meter:** run a fresh scan and confirm the device's local status exposes a power-meter component; then test it in **Energy → Settings**.
- **Energy meter is stale/offline:** verify its current inventory IP, credential profile, VLAN route, and request timeout. The collector retries automatically and records the transition.
- **Cost differs from the bill:** confirm price/VAT/excise settings, billing-cycle start day, timezone, and pricing effective timestamp. Fixed invoice charges and exported energy are intentionally excluded.
- **A room has no data:** confirm its assignment is enabled and effective for the selected period, its component test succeeds, its role is Primary, and its latest sample is fresh. A Submeter is intentionally excluded from the room total.
- **A room is partial or stale:** one or more expected primary sources is missing or older than twice its poll interval. Check the device IP, credentials, network route, and meter status; available source data remains visible and is not presented as complete.
- **Room meters exceed the main meter:** open **Energy → Reconciliation** and look for a circuit meter and its appliance meters both marked Primary, a three-phase total combined with individual phases, overlapping effective dates, resets, clock differences, or an export channel selected as import. The application preserves the mismatch and normalizes cost allocation; it does not silently edit assignments.
- **Coverage is lower than expected:** Unmetered / Other includes every load seen by the main meter but not by reporting room primaries, including genuine unmetered circuits and measurement gaps. Compare the reconciliation chart and missing-source list before adding another Primary.
- **A moved source appears in its old room:** reports use the assignment effective at the time of consumption. Check the selected reporting range and the move's effective timestamp; earlier readings deliberately stay with the old room.
- **Compose refuses to start:** export `APP_ENCRYPTION_KEY` before running Compose.

## Known limitations and future extensions

The MVP is IPv4-only, assumes the Shelly local HTTP API is reachable on its standard port, supports one whole-house meter, runs one scheduler/shared collector instance, and has no application authentication or TLS termination. Compose therefore publishes to loopback by default; wider access requires an authenticating TLS reverse proxy or an explicitly trusted management network plus `APP_PUBLISHED_ADDRESS` and `APP_ALLOWED_HOSTS` configuration. mDNS is best effort. Per-room figures require user-installed meters and are measured allocation, not automatic load disaggregation. Old coarse aggregates can only approximate attribution across an expired minute-level assignment boundary. The application intentionally does not control switches, lights, covers, gates, firmware, or device configuration. Demand tariffs, fixed invoice charges, budgets, alerts, solar/battery allocation, Home Assistant synchronization, multi-user access, and a public external API remain future extensions.
