# Running a node

A Freedom Names node runs a libp2p DHT peer, a DNS server, and an HTTP API all at
once. This page gets one running on your machine.

## Prerequisites

- **Go** (a recent version) to build and run the binary.
- Optionally, permission to bind port `:53` for DNS (see [below](#the-53-port)).

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
- a **DNS server** (default `:53`) that resolves `.fn` names and forwards
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

Port `:53` is privileged, so a plain `go run .` may not be able to bind it. Two
options:

**During development**, use a high port:

```sh
FREEDOM_DNS_ADDR=127.0.0.1:15353 go run .
```

**For a real deployment**, build the binary and grant it the capability once:

```sh
go build -o freedom-names .
sudo setcap cap_net_bind_service=+ep ./freedom-names
./freedom-names
```

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
