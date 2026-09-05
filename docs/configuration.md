# Configuration

Configuration is read from environment variables at startup. Settings saved on an individual Network override the timeout, scan interval, and concurrency defaults.

## Environment variables

| Variable | Default | Purpose |
|---|---:|---|
| `APP_LISTEN_ADDRESS` | `127.0.0.1:8080` | HTTP bind address; Compose overrides this inside the container |
| `APP_DATABASE_PATH` | `./data/shelly-inventory.db` | SQLite database file |
| `DATABASE_USERNAME` | empty | PostgreSQL user; set together with the other `DATABASE_*` values to use PostgreSQL |
| `DATABASE_PASSWORD` | empty | PostgreSQL password |
| `DATABASE_HOSTNAME` | empty | PostgreSQL hostname |
| `DATABASE_PORT` | `5432` | PostgreSQL port |
| `DATABASE_DB` | empty | PostgreSQL database name |
| `DATABASE_SSLMODE` | `disable` | PostgreSQL TLS mode; use `require` or stronger for a remote database |
| `APP_ENCRYPTION_KEY` | empty | Required before saving credentials; raw or base64-encoded 32 bytes |
| `APP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `APP_ENABLE_MDNS` | `false` | Browse `_shelly._tcp` and `_http._tcp`; candidates remain CIDR-restricted |
| `APP_ALLOW_NON_PRIVATE_NETWORKS` | `false` | Advanced opt-in for explicitly configured non-RFC1918 CIDRs |
| `APP_MAX_SUBNET_PREFIX` | `16` | Largest allowed IPv4 range; prefix must be `/16` or narrower |
| `APP_MAX_HOSTS` | `65534` | Absolute usable-address cap |
| `APP_DEFAULT_SCAN_CONCURRENCY` | `32` | Worker default for a new network |
| `APP_DEFAULT_CONNECTION_TIMEOUT` | `750ms` | TCP connection default |
| `APP_DEFAULT_REQUEST_TIMEOUT` | `2s` | Whole Shelly request default |
| `APP_OFFLINE_FAILURE_THRESHOLD` | `3` | Completed missed scans before a device is marked offline |
| `APP_MAX_CONCURRENT_SCANS` | `4` | Process-wide concurrent scan cap |
| `APP_ALLOWED_HOSTS` | `localhost,127.0.0.1,[::1]` | Accepted HTTP Host names/IPs; use `*` only behind a trusted proxy |
| `APP_SECURE_COOKIES` | `false` | Force the CSRF cookie's Secure flag when TLS terminates upstream |
| `APP_PUBLISHED_ADDRESS` | `127.0.0.1` | Compose-only address used to publish port 8080 |

> [!NOTE]
> The stock Compose file forwards the documented settings except `APP_MAX_HOSTS` and `APP_OFFLINE_FAILURE_THRESHOLD`. To change either in Compose, add it under the service's `environment` section in a local override file; exporting it only in the host shell does not pass it through automatically.

## VLAN routing and firewall policy

The container does not need to share the Shelly VLAN, but its Docker host must be able to route to it:

```text
Shelly Energy Dashboard container
        │ Docker bridge / host routing
        ▼
Docker host
        │ routed TCP/80 (or the device's local API port)
        ▼
IoT VLAN ──> Shelly devices
```

Allow established traffic and outbound TCP port 80 from the Docker subnet or host to the configured Shelly CIDR. Do not expose the Shelly VLAN to arbitrary internet sources.

If the host cannot connect to `http://DEVICE_IP/shelly`, a bridge container normally cannot either. Check inter-VLAN ACLs, host routes, Docker forwarding/firewall rules, and reverse-path filtering.

Active scanning works through a normal Docker bridge when routing allows it. mDNS generally does not cross routed VLANs or Docker bridges; it may require host networking, the same Layer-2 segment, or an mDNS reflector. Keep active CIDR discovery enabled as the reliable fallback.

## Shelly authentication

Set `APP_ENCRYPTION_KEY`, then create a reusable profile in **Settings** and assign it on a device page. Passwords are encrypted with AES-256-GCM and a random nonce. The browser receives only the profile name and username.

The local client supports Gen1 HTTP Basic authentication and HTTP Digest challenges used by newer Shelly devices.

Do not enter credentials over untrusted plaintext HTTP. For access beyond localhost:

1. Terminate TLS at an authenticating reverse proxy.
2. Keep the Compose port bound to loopback or a proxy-only interface.
3. Set `APP_ALLOWED_HOSTS` to the public management hostname.
4. Set `APP_SECURE_COOKIES=true`.

Use a strong, unique password per device. Digest responses remain susceptible to offline guessing if an old device IP is reassigned to a malicious host, so DHCP reservations are strongly recommended for authenticated devices.

The encryption key protects a copied database; it cannot protect credentials from a compromised running process. Back up the key separately from the SQLite database.

See [Security](security.md) for the full trust model.

---

[Documentation index](README.md) · [Getting started](getting-started.md) · [Security](security.md)
