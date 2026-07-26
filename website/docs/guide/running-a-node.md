# Run Freedom Names

Running Freedom Names starts a local libp2p DHT peer, content service, DNS
server, and HTTP API. This page explains how to start the application, verify
it, and optionally connect it to other peers.

## Download a release

Prebuilt releases are available for Linux, macOS, and Windows on both amd64 and
arm64. Download the package matching your platform from either:

- [GitLab releases](https://gitlab.melroy.org/freedom-names/freedom-names/-/releases)
  under **Assets → Packages**, or
- [GitHub releases](https://github.com/FreedomNames/FreedomNames/releases) under
  **Assets**.

For example, on 64-bit Linux, download
`freedom-names-0.9.1-linux-amd64.tar.gz` and extract it. Replace `0.9.1` with
the version shown on the release page when a newer release is available:

```sh
tar -xzf freedom-names-0.9.1-linux-amd64.tar.gz
```

Choose `linux-arm64` for 64-bit ARM Linux, `darwin-amd64` for an Intel Mac,
`darwin-arm64` for Apple Silicon, or the corresponding Windows `.zip` package.
Windows users can extract the zip with **Extract All** in File Explorer.

No installation or elevated privileges are required. Keep the binary wherever
you want to run it; the DNS server uses the unprivileged port `:8053` by
default.

## Start Freedom Names

```sh
./freedom-names
```

On Windows, run `.\freedom-names.exe` in PowerShell instead.

That single command starts:

- a **libp2p DHT peer**, the decentralized storage/resolution network,
- a **content service** that stores and retrieves page bytes over libp2p,
- a **DNS server** (default `:8053`) that resolves `.fn` names for anyone and
  forwards everything else upstream for local clients,
- an **HTTP API** (default `:8420`) for publishing and resolving.

You now have a working local Freedom Names instance. Leave it running in a
terminal; the CLI and your system resolver talk to it.

::: warning Network connectivity
Freedom Names discovers other instances on your local network through mDNS, and
dials the built-in default bootstrap peers to reach nodes beyond it. That public
network is still small, so expect few peers. Publishing and resolving DHT records
requires at least one other peer; check `/peers` to confirm you have one. To run
discovery you control, set `FREEDOM_BOOTSTRAP` to your own bootstrap node as
described below.
:::

## Run a bootstrap node

A **bootstrap** node is a server-mode peer that others connect to in order to
discover the network:

```sh
./freedom-names bootstrap
```

It uses fixed p2p ports (`4020`/`4021`/`4022`), serves its HTTP API on
`127.0.0.1:8430` instead of `8420`, and starts no DNS server. The different API
port means a bootstrap node and a normal node can run on the same machine
without either failing to bind. See
[Run a bootstrap node](/examples/bootstrap-node) for the full walkthrough.

Point other Freedom Names instances at it with the `FREEDOM_BOOTSTRAP`
environment variable (a comma-separated list of multiaddrs). See
[Configuration](/guide/configuration).

## The `:53` port

By default the DNS server listens on the high port **`:8053`**, so Freedom Names
works with no privileges. Query it with `dig -p 8053 …`.

Your OS and browser, however, only send DNS to the standard **`:53`**. For
system-wide `.fn` resolution on Linux, run Freedom Names on `:53` and grant the
downloaded binary the capability once:

```sh
sudo setcap cap_net_bind_service=+ep ./freedom-names
FREEDOM_DNS_ADDR=:53 ./freedom-names
```

Alternatively, keep `:8053` and forward `:53 → 127.0.0.1:8053` with a local
resolver (dnsmasq / systemd-resolved).

If the DNS port fails to bind, the node logs a warning and keeps running; the
DHT and HTTP API are unaffected.

## Verify it's up

Ask the node about itself over the HTTP API:

```sh
curl http://localhost:8420/info
```

You'll get JSON describing the node's mode, peer ID, listen addresses, and an
estimate of the network size. A just-started node may briefly return `500` here
until the DHT initializes; [`/health`](/guide/http-api#get-health) is an
always-on liveness check that answers even before then. See the [HTTP API
reference](/guide/http-api) for every endpoint.

## Optional: silence QUIC buffer warnings

libp2p uses QUIC, which may warn about small kernel receive buffers. Raise the
limits:

```sh
sudo sysctl -w net.core.rmem_max=7500000
sudo sysctl -w net.core.wmem_max=7500000
```

Make it permanent in `/etc/sysctl.conf`:

```ini
net.core.rmem_max=7500000
net.core.wmem_max=7500000
```

## Next

- [**Publish your first name**](/guide/your-first-name).
- [**Point your system at the node**](/guide/resolving) so `.fn` resolves
  everywhere.
