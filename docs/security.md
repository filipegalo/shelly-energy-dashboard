# Security

## Trust model

Shelly Energy Dashboard is designed for a trusted management network. It currently has no application-level user accounts or TLS termination. For remote or shared access, place it behind an authenticating TLS reverse proxy.

The application needs outbound access to configured Shelly networks and write access to its SQLite data directory. The browser never needs access to the IoT VLAN.

## Built-in controls

- Only explicitly configured networks are scanned; public ranges are rejected by default.
- URLs and device-selected paths are not accepted for discovery.
- Device HTTP redirects and environment HTTP proxies are disabled.
- Connections use the validated literal target address, closing DNS-rebinding and redirect-based SSRF paths.
- CIDR size, worker counts, timeouts, headers, forms, JSON bodies, and response sizes are bounded; Shelly response bodies are capped at 1 MiB.
- Templates escape device strings. Raw JSON is rendered as text, never trusted HTML.
- Mutations require a SameSite Strict double-submit CSRF token.
- CSP, framing, MIME, referrer, and permissions headers are set.
- SQL is parameterized. SQLite foreign keys, busy timeout, WAL, and a one-active-scan partial unique index are enabled.
- Logs omit passwords, cookies, authorization headers, response bodies, and complete authenticated requests.

## Credential protection

Saved passwords use AES-256-GCM with a random nonce. The browser sees only the credential profile name and username.

`APP_ENCRYPTION_KEY` must contain exactly 32 raw bytes or base64-decode to 32 bytes. Losing or changing it makes existing ciphertext undecryptable. Back it up separately from SQLite.

Encryption protects credentials in a copied database. It cannot protect them from a compromised running process. A unique device password and DHCP reservation reduce the risk of stale-address credential exposure.

## Deployment checklist

- [ ] Bind the application to loopback or a proxy-only interface.
- [ ] Terminate TLS and require authentication before access beyond localhost.
- [ ] Set `APP_ALLOWED_HOSTS` to the management hostname.
- [ ] Set `APP_SECURE_COOKIES=true` behind HTTPS.
- [ ] Use a strong, unique 32-byte `APP_ENCRYPTION_KEY`.
- [ ] Restrict outbound firewall access to required Shelly CIDRs and ports.
- [ ] Avoid exposing the Shelly VLAN to arbitrary internet sources.
- [ ] Use strong unique credentials and DHCP reservations for authenticated devices.
- [ ] Protect both backups and the separately stored encryption key.
- [ ] Monitor `/ready`, errors, and stale/offline meter state.

The Compose definition also uses a read-only container filesystem, a bounded tmpfs, dropped Linux capabilities, `no-new-privileges`, and a process limit.

---

[Documentation index](README.md) · [Configuration](configuration.md) · [Operations](operations.md)
