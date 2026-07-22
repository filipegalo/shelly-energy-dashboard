# Troubleshooting

## Discovery and networking

**No devices found**  
Run `curl http://DEVICE_IP/shelly` from the Docker host. Then check the configured CIDR, enabled state, VLAN route, ACLs, and TCP port 80.

**A scan has many timeouts**  
Lower per-network concurrency on congested Wi-Fi, or increase the connection and request timeouts.

**A device becomes offline**  
By default it must be absent from three completed scans. Cancelled and failed scans do not age device status.

**mDNS finds nothing**  
This is expected across many routed VLANs and Docker bridge networks. Use CIDR scanning or deploy an mDNS reflector.

**Authentication required persists**  
Assign the correct credential profile, then refresh the device. Shelly usernames are commonly `admin`, but the device's local configuration is authoritative.

## Storage and startup

**Database busy**  
Ensure only one application instance uses the database and `/data` is persistent local storage rather than a high-latency network filesystem.

**Compose refuses to start**  
Export `APP_ENCRYPTION_KEY` before running Compose. It must be 32 raw bytes or base64-encoded 32 bytes.

## Whole-house energy

**No eligible meter appears**  
Run a fresh scan and confirm the local device status exposes a supported power-meter component, then test it in **Energy → Settings**.

**The meter is stale or offline**  
Verify its current inventory IP, credential profile, VLAN route, and request timeout. The collector retries automatically and records state transitions.

**Estimated cost differs from the bill**  
Confirm price, VAT, excise, billing-cycle start, timezone, and the pricing effective timestamp. Fixed invoice charges and exported energy are intentionally excluded.

## Room energy

**A room has no data**  
Confirm the assignment is enabled and effective for the selected period, its component test succeeds, its role is Primary, and the latest sample is fresh. Submeters do not contribute to room totals.

**A room is partial or stale**  
One or more expected primary sources is missing or older than twice its poll interval. Check IP, credentials, route, and meter state. Available source data remains visible and is not labelled complete.

**Room meters exceed the main meter**  
Open **Energy → Reconciliation**. Look for a circuit and its appliance meters both marked Primary, a three-phase total combined with individual phases, overlapping effective dates, resets, clock differences, or an export channel selected as import. The application preserves the mismatch and normalizes cost allocation; it does not silently rewrite assignments.

**Coverage is lower than expected**  
Unmetered / Other includes every load seen by the main meter but not by reporting room primaries, including genuine unmetered circuits and measurement gaps. Compare the reconciliation chart and missing-source list before adding another Primary.

**A moved source appears in its old room**  
Reports use the assignment effective at the time of consumption. Check the reporting range and move timestamp; earlier readings deliberately stay with the old room.

## Current limitations

- IPv4 only
- Shelly local HTTP API expected on its standard port
- One whole-house meter per deployment
- One scheduler/shared collector instance and one app instance per SQLite file
- No application authentication or TLS termination
- mDNS is best-effort and often cannot cross VLANs or Docker bridges
- Per-room results require installed meters; they are measured allocation, not automatic load disaggregation
- Old coarse aggregates can only approximate attribution across an expired minute-level assignment or pricing boundary
- Monitoring only: no switch, light, cover, gate, firmware, or device-configuration control
- No demand tariffs, fixed invoice charges, budgets, alerts, solar/battery allocation, Home Assistant synchronization, multi-user access, or public external API

Compose binds to loopback by default. Wider access requires an authenticating TLS reverse proxy or an explicitly trusted management network plus `APP_PUBLISHED_ADDRESS` and `APP_ALLOWED_HOSTS` configuration.

---

[Documentation index](README.md) · [Configuration](configuration.md) · [Development](development.md)
