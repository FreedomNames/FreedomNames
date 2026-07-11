# Running a node

A Freedom Names node runs a libp2p DHT peer, a DNS server, and an HTTP API all at
once. This page gets one running on your machine.

## Prerequisites

- **Go** (a recent version) to build and run the binary.
- No special privileges — the DNS server defaults to the high port `:8053`. (For
  system-wide `.fn` resolution on `:53`, see [below](#the-53-port).)

Clone the repository:

```sh
git clone https://gitlab.melroy.org/freedom-names/freedom-names.git
cd freedom-names
```

## Start a node

```sh
go run .
```

That single command starts:

- a **libp2p DHT peer**, the decentralized storage/resolution network,
- a **DNS server** (default `:8053`) that resolves `.fn` names and forwards
  everything else upstream,
- an **HTTP API** (default `:8080`) for publishing and resolving.

You now have a working node. Leave it running in a terminal; the CLI and your
system resolver talk to it.

## Run a bootstrap node

A **bootstrap** node is a server-mode peer that others connect to in order to
discover the network:

```sh
go run . bootstrap
```

Point other nodes at it with the `FREEDOM_BOOTSTRAP` environment variable (a
comma-separated list of multiaddrs). See [Configuration](/guide/configuration).

## The `:53` port

By default the DNS server listens on the high port **`:8053`**, so `go run .`
works with no privileges. Query it with `dig -p 8053 …`.

Your OS and browser, however, only send DNS to the standard **`:53`**. For
system-wide `.fn` resolution, run Freedom Names on `:53` — build the binary and
grant it the capability once:

```sh
go build -o freedom-names .
sudo setcap cap_net_bind_service=+ep ./freedom-names
FREEDOM_DNS_ADDR=:53 ./freedom-names
```

Alternatively, keep `:8053` and forward `:53 → 127.0.0.1:8053` with a local
resolver (dnsmasq / systemd-resolved).

If the DNS port fails to bind, the node logs a warning and keeps running — the
DHT and HTTP API are unaffected.

## Verify it's up

Ask the node about itself over the HTTP API:

```sh
curl http://localhost:8080/info
```

You'll get JSON describing the node's mode, peer ID, listen addresses, and an
estimate of the network size. See the [HTTP API reference](/guide/http-api) for
every endpoint.

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

## Optional: auto-recompile with air

Install [air](https://github.com/air-verse/air) to rebuild on save while
developing:

```sh
air
```

## Next

- [**Publish your first name**](/guide/your-first-name).
- [**Point your system at the node**](/guide/resolving) so `.fn` resolves
  everywhere.
