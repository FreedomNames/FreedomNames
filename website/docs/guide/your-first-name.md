# Your first name

This walkthrough takes you from nothing to a resolvable `.fn` name in five steps.
It assumes you have a [node running](/guide/running-a-node) on
`http://localhost:8420`.

::: tip
Invoke the CLI via the built binary (`./freedom-names freedom keygen mysite`)
or, during development, `go run . freedom keygen mysite`. On this page we write it
as `freedom …` for brevity.
:::

## 1. Generate an owner keypair

```sh
freedom keygen mysite
```

```
Generated key for "mysite"
Your name: mysite.<pubKeyID>.fn
```

This creates an Ed25519 keypair under `~/.freedom/keys/mysite.key`. The
`<pubKeyID>` in the output is derived from the public key; that's your
self-certifying name. Keep the key file safe: **it is the name.**

## 2. Stage some records

Records are staged locally first, so you can review the whole set before signing:

```sh
freedom set mysite A 10.0.0.5 300
freedom set mysite TXT "hello world"
```

Each `set` appends a resource record and validates the whole set eagerly, so
mistakes (a malformed IP, an unsupported type) surface now rather than at publish
time. The trailing number on the `A` record is the TTL in seconds (default 300).

Supported types: `A`, `AAAA`, `TXT`, `CNAME`.

Staged records live in `~/.freedom/keys/mysite.records.json`. To start over:

```sh
freedom clear mysite
```

## 3. See your full name

```sh
freedom name mysite
```

```
mysite.<pubKeyID>.fn
```

You'll use this full name to resolve. Copy it somewhere handy.

## 4. Sign and publish

```sh
freedom publish mysite --api http://localhost:8420
```

```
Published mysite.<pubKeyID>.fn (seq 1720713600, 2 record(s))
```

Under the hood the CLI signs the staged records with your private key, derives a
sequence number from the current time (so republishes always supersede older
ones), and POSTs the signed record to the node's `/publish` endpoint. The node
**verifies** it before storing it in the DHT. If the signature or key binding
were wrong, it would be rejected.

::: info
`--api` defaults to `http://localhost:8420`, so you can omit it when publishing to
a local node.
:::

## 5. Resolve it

Over the HTTP API, via the CLI:

```sh
freedom lookup mysite.<pubKeyID>.fn --type A
```

Or straight over DNS, like any resolver:

```sh
dig @127.0.0.1 -p 8053 mysite.<pubKeyID>.fn A
```

Either way you get back the `A` record you published. 🎉

## Republishing after a change

Edit your records and publish again, and the new sequence number wins:

```sh
freedom set mysite A 10.0.0.9 300
freedom publish mysite
```

No re-registration, no waiting for propagation through a registrar. The update is
a new signed record that supersedes the old one across the network.

## Next

- Make `.fn` resolve everywhere by [pointing your system at the
  node](/guide/resolving).
- Try a full [example: host a website on `.fn`](/examples/host-a-website).
- Reference: the complete [CLI](/guide/cli) and [HTTP API](/guide/http-api).
