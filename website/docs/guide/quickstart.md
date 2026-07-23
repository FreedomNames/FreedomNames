# Quickstart

Get a Freedom Names node running locally in a couple of minutes. For platform
details, peer configuration, and troubleshooting, see [**Run Freedom
Names**](/guide/running-a-node).

## 1. Download

Grab the prebuilt archive for your OS and architecture from either release page:

- [GitLab releases](https://gitlab.melroy.org/freedom-names/freedom-names/-/releases)
  under **Assets → Packages**, or
- [GitHub releases](https://github.com/FreedomNames/FreedomNames/releases) under
  **Assets**.

Builds are available for Linux, macOS, and Windows on both amd64 and arm64.

## 2. Extract and run

On 64-bit Linux (replace `0.8.4` with the version you downloaded):

```sh
tar -xzf freedom-names-0.8.4-linux-amd64.tar.gz
./freedom-names
```

On Windows, run `.\freedom-names.exe` instead. No installation or elevated
privileges are needed; the DNS server uses the unprivileged port `:8053`.

Leave that terminal open; the CLI and your system resolver talk to it.

## 3. Verify

In a second terminal, check the local HTTP API:

```sh
curl http://localhost:8420/health
```

A `{"status":"ok",...}` response means the node is up. You now have a working
local instance.

## What's next

- Nearby nodes discover each other over mDNS, but publishing and resolving
  through the DHT needs at least one other peer. On a single machine, run a
  second node as a bootstrap in another terminal (see [**Run a bootstrap
  node**](/guide/running-a-node#run-a-bootstrap-node)), or point at an existing
  peer with `FREEDOM_BOOTSTRAP` (see [**Run Freedom
  Names**](/guide/running-a-node)).
- Ready to create a name? Continue with [**Your first
  name**](/guide/your-first-name).
- Make `.fn` resolve system-wide: [**Resolving from your
  system**](/guide/resolving).
