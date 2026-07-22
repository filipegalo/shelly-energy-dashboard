# Development

## Toolchain

- Go 1.24+
- Node.js 22+ for frontend asset compilation
- SQLite CLI only for manual local backup commands

## Build and run

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

Equivalent Make targets:

```bash
make assets
make fmt
make test
make vet
make build
make run
make docker-build
make compose-up
make mocks
```

## Mock devices

The development mock covers Gen1, Gen2, Gen3, Digest-protected, malformed, slow, unsupported-RPC, stable-identity/IP-change, deterministic three-phase energy, and room-metering profiles.

Start the application and mocks on a dedicated Docker subnet:

```bash
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker compose -p shelly-energy-dev \
  -f docker-compose.yml -f docker-compose.mock.yml up -d --build
docker network connect shelly-energy-dev_shelly-mocks shelly-inventory
```

The explicit project name keeps the generated mock-network name stable regardless of the checkout directory.

Add `172.28.0.0/27` as an enabled network in the UI.

For a single local mock:

```bash
MOCK_PROFILE=gen2 MOCK_LISTEN_ADDRESS=127.0.0.1:9080 go run ./cmd/mock-shelly
```

The production scanner intentionally rejects arbitrary ports and loopback targets. The local command is for client development, not a production-policy scan.

## Whole-house fixtures

Energy mocks use `172.28.0.11` through `172.28.0.15`. After scanning, select `mock-energy` and component `em:0`.

- `mock-energy-reset` rolls its imported counter back after two reads.
- `mock-energy-invalid` reports a CT configuration error.
- `mock-energy-slow` exceeds normal request timeouts.
- `mock-energy-flaky` transitions from valid readings to a disconnected meter.

## Room-energy fixtures

Room fixtures use `172.28.0.16` through `172.28.0.29`. Every successful component read advances its cumulative counter by one 10-second share of its documented daily energy, keeping repeated test runs deterministic.

| Address | Profile | Intended use |
|---|---|---|
| `172.28.0.16` | `room-main3em` | Three-phase main meter at 30 kWh/day |
| `172.28.0.17` | `room-pro4pm` | Kitchen 10, Living Room 5, Office 4, Garage 2 kWh/day |
| `172.28.0.18` | `room-em2` | Alternative Kitchen 10 and Living Room 5 kWh/day meter |
| `172.28.0.19` | `room-plug-dishwasher` | Kitchen dishwasher submeter at 3 kWh/day |
| `172.28.0.20` | `room-plug-refrigerator` | Kitchen refrigerator submeter at 2 kWh/day |
| `172.28.0.21`–`.23` | room smart plugs | Living Room 5, Office 4, Garage 2 kWh/day alternatives |
| `172.28.0.24` | `room-source-offline` | Goes offline after two successful energy reads |
| `172.28.0.25` | `room-source-stale` | Retains one reading, then stops updating while reachable |
| `172.28.0.26` | `room-source-reset` | Resets its cumulative counter on the third read |
| `172.28.0.27` | `reconcile-below` | Room channels total 21 kWh/day against a 30 kWh/day main meter |
| `172.28.0.28` | `reconcile-slight-over` | Room channels total 30.6 kWh/day |
| `172.28.0.29` | `reconcile-significant-over` | Room channels total 36 kWh/day |

For the baseline reconciliation scenario, use `.16` component `em:0` as the main meter and `.17` components `switch:0` through `switch:3` as four room primaries. They total 21 kWh/day, leaving 9 kWh/day as Unmetered / Other.

Add `.19` and `.20` as submeters beneath Kitchen `switch:0`. Together they explain 5 kWh/day of the 10 kWh/day Kitchen primary without increasing its total.

The `.18` and `.21`–`.23` fixtures are alternative sources; do not configure them simultaneously as primaries over the same loads. To test over-measurement, replace `.17` assignments with matching channels from `.28` or `.29` rather than adding both sets.

The mock exposes only fixed Shelly status RPC endpoints and validates component IDs, so it cannot proxy arbitrary URLs or methods.

---

[Documentation index](README.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md)
