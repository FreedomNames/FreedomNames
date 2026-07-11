# CLI reference

The `freedom` CLI generates keys, stages records, publishes them to a running
node, and resolves names. Keys and staged records live under
`~/.freedom/keys/`.

Invoke it via the built binary or during development with `go run .`:

```sh
./freedom-names freedom keygen mysite
# or
go run . freedom keygen mysite
```

The default node API is `http://localhost:8080` (override with `--api`).

## Commands at a glance

| Command | Purpose |
| --- | --- |
| `freedom keygen <label>` | Generate an owner keypair for a name |
| `freedom set <label> <TYPE> <VALUE> [ttl]` | Stage a resource record (`A`\|`AAAA`\|`TXT`\|`CNAME`) |
| `freedom clear <label>` | Remove all staged records for a name |
| `freedom name <label>` | Print the full `label.<pubKeyID>.fn` name |
| `freedom publish <label> [--api URL]` | Sign staged records and publish to a node |
| `freedom lookup <name> [--api URL] [--type TYPE]` | Resolve a name via a node |
| `freedom help` | Show usage |

## `freedom keygen <label>`

Generates an Ed25519 keypair for a name and writes it to
`~/.freedom/keys/<label>.key` (mode `0600`). Prints your full self-certifying
name.

```sh
freedom keygen mysite
```

```
Generated key for "mysite"
Your name: mysite.<pubKeyID>.fn
```

Fails if a key for that label already exists; it will not overwrite your key.

## `freedom set <label> <TYPE> <VALUE> [ttl]`

Stages one resource record for a name. `TTL` is in seconds and defaults to `300`.
The full staged set is validated eagerly, so invalid values are rejected
immediately.

```sh
freedom set mysite A 10.0.0.5 300
freedom set mysite AAAA 2001:db8::1
freedom set mysite TXT "v=spf1 -all"
freedom set mysite CNAME target.example.com
```

Staged records accumulate in `~/.freedom/keys/<label>.records.json`.

**Supported types:** `A` (IPv4), `AAAA` (IPv6), `TXT` (any UTF-8), `CNAME`
(non-empty target).

## `freedom clear <label>`

Removes all staged records for a name (does not touch the key).

```sh
freedom clear mysite
```

## `freedom name <label>`

Prints the full `label.<pubKeyID>.fn` name derived from the label's key.

```sh
freedom name mysite
# mysite.<pubKeyID>.fn
```

## `freedom publish <label> [--api URL]`

Signs the staged records with the label's private key and POSTs the signed record
to a node's `/publish` endpoint. The sequence number is derived from the current
time so republishes monotonically increase and supersede older records.

```sh
freedom publish mysite --api http://localhost:8080
```

```
Published mysite.<pubKeyID>.fn (seq 1720713600, 2 record(s))
```

Fails if there are no staged records, or if the node rejects the record (e.g. it
fails verification).

## `freedom lookup <name> [--api URL] [--type TYPE]`

Resolves a full name via a node's `/resolve` endpoint and prints the JSON
response. Optionally filter by record type.

```sh
freedom lookup mysite.<pubKeyID>.fn
freedom lookup mysite.<pubKeyID>.fn --type A
```

## Where things live

| Path | Contents |
| --- | --- |
| `~/.freedom/keys/<label>.key` | the owner private key for a name |
| `~/.freedom/keys/<label>.records.json` | staged records awaiting publish |

The node's own libp2p identity (`private.key`) is **separate**, so your names are
portable between nodes.

## Next

- The [**HTTP API**](/guide/http-api) the CLI talks to.
- A full walkthrough: [**your first name**](/guide/your-first-name).
