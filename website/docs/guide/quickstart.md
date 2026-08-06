# Quickstart

Freedom Names has **two quick-start paths**. Choose the one that matches the
job:

| Run | Command | Use it for |
|---|---|---|
| **Normal node** | `curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh \| sudo bash -s -- normal` | A Debian/Ubuntu computer: DNS, HTTP API, and a foreground process. No systemd. |
| **Bootstrap node** | `curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh \| sudo bash -s -- bootstrap` | A public Debian/Ubuntu server that helps other nodes join and starts on boot. |

## Bootstrap node: install and start

For a public bootstrap server, this one command downloads a verified release,
installs the systemd service, and enables + starts it:

```sh
curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh | \
  sudo bash -s -- bootstrap
```

It runs `freedom-names-bootstrap`, keeps its identity at
`/home/freedom/.freedom/private.key`, and exposes its health endpoint only on
the local machine:

```sh
curl --fail http://127.0.0.1:8430/health
```

A bootstrap node is not a resolver: it has no DNS listener. It is the
long-running rendezvous server that normal nodes connect to. Open the required
p2p ports and back up its identity key as described in [Run a bootstrap
node](/examples/bootstrap-node).

## Normal node: install and start

On Debian or Ubuntu, install the latest verified release, then run it in the
foreground:

```sh
curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh | \
  sudo bash -s -- normal
```

```sh
freedom-names
```

The installer installs only the executable; it does not create a service.

## Manual installation

If you are not using Debian or Ubuntu, download the prebuilt archive and run it
in the foreground whenever you need it.

### 1. Download

Grab the prebuilt archive for your OS and architecture from either release page:

- [GitLab releases](https://gitlab.melroy.org/freedom-names/freedom-names/-/releases)
  under **Assets → Packages**, or
- [GitHub releases](https://github.com/FreedomNames/FreedomNames/releases) under
  **Assets**.

Builds are available for Linux, macOS, and Windows on both amd64 and arm64.

### 2. Extract and run

On 64-bit Linux (replace `0.9.5` with the version you downloaded):

```sh
tar -xzf freedom-names-0.9.5-linux-amd64.tar.gz
./freedom-names
```

Releases ship a `SHA256SUMS` file too, so you can check the download first with
`sha256sum -c SHA256SUMS --ignore-missing`.

On Windows, run `.\freedom-names.exe` instead. No installation or elevated
privileges are needed; the DNS server uses the unprivileged port `:8053`.

Leave that terminal open; the CLI and your system resolver talk to it.

### 3. Verify

In a second terminal, check the local HTTP API:

```sh
curl http://localhost:8420/health
```

A `{"status":"ok",...}` response means the node is up. You now have a working
local instance.

## What's next

- **Normal node:** it joins the network on its own: it dials the built-in public
  bootstrap peers, and finds nearby instances over mDNS. Confirm with `curl -s
  localhost:8420/peers`, which should list at least one peer within a few
  seconds. To use peers you control instead, set `FREEDOM_BOOTSTRAP` (see [**Run
  a bootstrap node**](/examples/bootstrap-node)).
- **Bootstrap node:** finish its operator setup in [**Run a bootstrap
  node**](/examples/bootstrap-node): collect the public multiaddr, open the p2p
  ports, and back up the peer identity.
- Ready to create a name? Continue with [**Your first
  name**](/guide/your-first-name).
- Make `.fn` resolve system-wide: [**Resolving from your
  system**](/guide/resolving).
