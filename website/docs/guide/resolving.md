# Resolving from your system

Once a node is running, it behaves like any other DNS server: it answers `.fn`
queries itself and forwards everything else to an upstream resolver. That means
you can make `.fn` "just work" system-wide by pointing your machine at the node.

## Query the node directly

The simplest test, asking the node by hand with `dig`:

```sh
dig @127.0.0.1 -p 8053 mysite.<pubKeyID>.fn A
```

Non-`.fn` queries are transparently forwarded upstream, so the same server can
answer for `example.com` too:

```sh
dig @127.0.0.1 -p 8053 example.com A
```

Two caveats:

- Forwarding uses plain UDP and does not retry truncated answers over TCP, so
  unusually large upstream responses can arrive truncated.
- Forwarding is only done for **local clients** (loopback, private and
  link-local addresses). Asking a node on a public IP to resolve `example.com`
  for you gets `REFUSED`, because forwarding for anyone would make it an
  [open resolver](/guide/configuration#who-the-dns-server-answers). `.fn`
  queries are answered for everyone. Set `FREEDOM_DNS_RECURSION=any` if you
  really do want a public forwarder.

This is what makes a Freedom Names node usable as your **only** resolver: it adds
`.fn` without breaking the rest of the internet.

## What answers to expect

- A `.fn` name answers with the records matching the query type. A `CNAME`
  record also answers `A` and `AAAA` queries (per RFC 1034), so CNAME-only
  names stay reachable through normal clients.
- A name that doesn't exist returns **NXDOMAIN**.
- A lookup that times out (a slow DHT walk) returns **SERVFAIL** instead:
  that's transient, so retrying can succeed where NXDOMAIN won't.

## Use it as your system resolver

To make `.fn` work in your browser and every app, point your OS at the node. The
node must be listening on the standard DNS port `:53` (see
[the `:53` port](/guide/running-a-node#the-53-port)).

::: warning
Configuring your system resolver is OS-specific and affects **all** DNS on the
machine. Test with `dig @127.0.0.1` first, and know how to revert.
:::

### Linux (systemd-resolved)

Set the node as the DNS server for an interface, or globally in
`/etc/systemd/resolved.conf`:

```ini
[Resolve]
DNS=127.0.0.1
Domains=~fn
```

Then restart the service:

```sh
sudo systemctl restart systemd-resolved
```

The `~fn` routing domain tells resolved to send `.fn` queries to this server
specifically, which is handy if you keep your normal resolver for everything else.

### Linux (plain resolv.conf)

If you don't use a resolver manager, point `/etc/resolv.conf` at the node:

```ini
nameserver 127.0.0.1
```

### macOS

Add a resolver for the `fn` TLD only:

```sh
sudo mkdir -p /etc/resolver
echo "nameserver 127.0.0.1" | sudo tee /etc/resolver/fn
```

macOS reads `/etc/resolver/<tld>` files and routes just `.fn` to your node,
leaving the rest of your DNS untouched.

### Router / network-wide

Set the node's IP as the DNS server in your router's DHCP settings to make `.fn`
resolve for every device on the network. Make sure the node is reachable and
stays running.

## Verify

Once configured, `.fn` should resolve without the explicit `@127.0.0.1`:

```sh
dig mysite.<pubKeyID>.fn A
# or just open it in a browser, if it points at a web server
```

## Next

- Put it to use: [**host a website on `.fn`**](/examples/host-a-website).
- Understand the forwarding behavior in the [**Architecture**](/guide/architecture).
