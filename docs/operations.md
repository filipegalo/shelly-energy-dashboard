# Operations

## Production baseline

- Keep the service bound to loopback or a proxy-only interface.
- Use an authenticating TLS reverse proxy for any access beyond a trusted management network.
- Persist `/data` on local durable storage.
- Keep exactly one application instance attached to a SQLite database.
- Store `APP_ENCRYPTION_KEY` separately from database backups.
- Pin a container digest when rollout immutability matters.

See [Configuration](configuration.md) for network routing and [Security](security.md) for the trust model.

## Health and readiness

| Endpoint | Meaning |
|---|---|
| `GET /health` | The process is serving HTTP |
| `GET /ready` | SQLite responds; returns `503` when unavailable |
| `GET /metrics` | Prometheus text exposition |

## Metrics

Discovery metrics cover scan counts and durations by terminal status, addresses scanned, discovered and new devices, local API durations and failures, device state gauges, and running scans.

Energy metrics cover request count, duration, and errors; latest-reading time; live watts; process-recorded kWh; reset and missing-sample counters; and current billing-cycle variable-cost estimate.

Room metrics cover selected, online, and stale sources plus aggregation, coverage, unmetered energy, reconciliation, and allocation health.

Labels deliberately exclude room names, device IDs, IP addresses, component keys, and scan IDs to bound cardinality.

## Back up SQLite

For a locally stored database, SQLite's online backup command produces a consistent WAL snapshot:

```bash
mkdir -p backups
sqlite3 ./data/shelly-inventory.db ".backup './backups/shelly-inventory-$(date +%F).db'"
```

For the Compose volume, stop the application and copy the database:

```bash
docker compose stop shelly-inventory
SHELLY_DATA_VOLUME="$(docker inspect shelly-inventory \
  --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}')"
docker run --rm \
  -v "${SHELLY_DATA_VOLUME}:/data" \
  -v "$PWD/backups:/backup" \
  alpine cp /data/shelly-inventory.db /backup/shelly-inventory.db
docker compose start shelly-inventory
```

## Restore SQLite

Restore only while the application is stopped:

```bash
docker compose stop shelly-inventory
SHELLY_DATA_VOLUME="$(docker inspect shelly-inventory \
  --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}')"
docker run --rm \
  -v "${SHELLY_DATA_VOLUME}:/data" \
  -v "$PWD/backups:/backup:ro" \
  alpine cp /backup/shelly-inventory.db /data/shelly-inventory.db
docker compose start shelly-inventory
```

Keep the encryption key with the recovery procedure, but never in the same unprotected location as the database backup.

## Upgrade considerations

Before an upgrade:

1. Record the currently deployed image digest or commit.
2. Back up the database and confirm the encryption key is available.
3. Pull or build the new image.
4. Restart one instance against the persistent volume.
5. Check `/ready`, logs, device discovery, and latest energy samples.

Database migrations are embedded in the application and run at startup. Do not run old and new application versions concurrently against the same SQLite file.

## Roll back safely

If an upgrade must be reversed, stop the application before changing the image. Application rollback and data rollback are separate decisions: a newer migration may not be understood by an older binary. The safest full rollback is to restore the pre-upgrade database backup, restore the prior image digest, then start exactly one instance and verify `/ready`.

Preserve the failed-upgrade database separately when it contains energy readings you may need to recover later.

---

[Documentation index](README.md) · [Configuration](configuration.md) · [Security](security.md)
