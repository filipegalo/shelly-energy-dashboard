# Getting started

This guide takes a new deployment from zero to a discovered network and an optional whole-house meter.

## Requirements

- Docker with the Compose plugin
- Layer-3 reachability from the Docker host to the Shelly device network
- A trusted management network, or an authenticating TLS reverse proxy

The application does not require Shelly Cloud access.

## Start with Docker Compose

Generate a strong 32-byte encryption key and start the application:

```bash
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker compose up -d --build
docker compose logs -f shelly-inventory
```

Open [http://localhost:8080](http://localhost:8080). Compose publishes on host loopback by default, and stores the database in the `shelly-data` volume at `/data/shelly-inventory.db` inside the container.

Keep the encryption key. Changing or losing it makes saved credential ciphertext undecryptable; store it separately from database backups.

## Discover devices

1. Open **Networks**.
2. Add an IPv4 CIDR that contains your Shelly devices, such as `192.168.20.0/24`.
3. Keep **Enable scanning** selected.
4. Choose **Scan**.

The live scan view resumes from the latest durable snapshot after a browser reconnect. Recurring scans run in the Go process and do not require an open browser.

If no devices appear, confirm that the Docker host itself can reach `http://DEVICE_IP/shelly`, then see [Troubleshooting](troubleshooting.md#discovery-and-networking).

## Tour the interface

- **Dashboard** summarizes online, offline, authentication-required, and unassigned devices.
- **Devices** provides inventory search and filters. Device details include normalized identity, network and firmware data, capabilities, credential assignment, diagnostics, and collapsible raw JSON.
- **Rooms** organizes physical inventory without coupling device identity to DHCP addresses.
- **Networks** is the only place that grants the scanner access to a CIDR.
- **Scan history** retains durable progress and errors.
- **Energy** shows live whole-house power, history, billing-cycle summaries, and variable-cost estimates.
- **Energy → Rooms** compares attributed room consumption and allocated cost, then reconciles it with the main meter.

## Configure energy monitoring

Scan the network before configuring a meter.

1. Open **Energy → Settings**.
2. Select a discovered device with power-meter capability.
3. Discover and test its meter component.
4. Save the configuration.

The collector runs in the server process and continues when no browser is open. After configuring the main meter, use **Energy → Room metering** to test and assign individual meter components or channels to rooms.

The [energy monitoring guide](energy-monitoring.md) explains supported components, retention, cost calculations, room roles, and reconciliation.

## Use the prebuilt image

Every push to `master` publishes an SBOM-enabled image for `linux/amd64` and `linux/arm64`. Public builds also include signed provenance.

```bash
docker pull ghcr.io/filipegalo/shelly-energy-dashboard:latest
```

Run it directly with a persistent volume and loopback-only binding:

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

The publisher also creates `sha-<commit>` tags. A stable tag such as `v1.2.3` publishes `1.2.3`, `1.2`, and `1`; prereleases such as `v1.2.3-rc.1` do not replace `latest`. Pin the published digest when strict immutability is required.

If the package is private, authenticate with `docker login ghcr.io` using a GitHub token with package read access.

## Stop or remove the deployment

```bash
docker compose down
```

Do not add `-v` unless you intentionally want Docker to delete the persistent data volume.

Use `docker compose logs -f shelly-inventory` to follow structured application logs.

Before production use, review [Configuration](configuration.md), [Security](security.md), and [Operations](operations.md).

---

[Documentation index](README.md) · [Configuration](configuration.md) · [Energy monitoring](energy-monitoring.md)
