# Quickstart

Get a Freedom Names node running locally in a couple of minutes. For platform
details, peer configuration, and troubleshooting, see [**Run Freedom
Names**](/guide/running-a-node).

::: tip Running infrastructure for others?
For a public bootstrap server that starts on boot, use the one-command
[bootstrap-server install](/examples/bootstrap-node#install-and-start) instead.
It installs a systemd service; ordinary local nodes do not need one.
:::

## 1. Download

Grab the prebuilt archive for your OS and architecture from either release page:

- [GitLab releases](https://gitlab.melroy.org/freedom-names/freedom-names/-/releases)
  under **Assets → Packages**, or
- [GitHub releases](https://github.com/FreedomNames/FreedomNames/releases) under
  **Assets**.

Builds are available for Linux, macOS, and Windows on both amd64 and arm64.

## 2. Extract and run

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

## 3. Verify

In a second terminal, check the local HTTP API:

```sh
curl http://localhost:8420/health
```

A `{"status":"ok",...}` response means the node is up. You now have a working
local instance.

## What's next

- Your node joins the network on its own: it dials the built-in public bootstrap
  peers, and finds nearby instances over mDNS. Confirm with `curl -s
  localhost:8420/peers`, which should list at least one peer within a few
  seconds. To use peers you control instead, set `FREEDOM_BOOTSTRAP` (see [**Run
  a bootstrap node**](/examples/bootstrap-node)).
- Ready to create a name? Continue with [**Your first
  name**](/guide/your-first-name).
- Make `.fn` resolve system-wide: [**Resolving from your
  system**](/guide/resolving).
- Want to help other nodes join the network? [**Run a bootstrap
  node**](/examples/bootstrap-node) on a public Linux server.
