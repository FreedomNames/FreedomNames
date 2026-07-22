# CLI reference

The `freedom` CLI generates keys, stages records, publishes them to a running
node, and resolves names. Keys and staged records live under
`~/.freedom/keys/`.

Invoke it through the downloaded binary:

```sh
./freedom-names freedom keygen mysite
```

The default node API is `http://localhost:8420` (override with `--api`).

## Commands at a glance

| Command | Purpose |
| --- | --- |
| `freedom keygen <label>` | Generate an owner keypair for a name |
| `freedom set <label> <TYPE> <VALUE> [ttl]` | Stage a resource record (`A`\|`AAAA`\|`TXT`\|`CNAME`\|`CONTENT`) |
| `freedom clear <label>` | Remove all staged records for a name |
| `freedom name <label>` | Print the full `label.<pubKeyID>.fn` name |
| `freedom publish <label> [--api URL]` | Sign staged records and publish to a node |
| `freedom put <label> <file> [--api URL] [--ttl S]` | Upload a file's content and point `<label>` at it |
| `freedom lookup <name> [--api URL] [--type TYPE]` | Resolve a name via a node |
| `freedom help` | Show usage (also `-h` / `--help`) |

Running `freedom` with no subcommand prints the usage and exits with code `2`;
an unknown subcommand prints it and exits with code `1`.

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
(non-empty target), `CONTENT` (a content hash, see
[the content network](/guide/content)).

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
to a node's `/publish` endpoint. The sequence number is chosen strictly above the
name's current record (fetched via `/record`, falling back to the current time)
so updates always supersede older records, even for same-second publishes or a
clock that stepped backwards. Records stay valid for 7 days; re-run publish
before then to renew (the CLI prints the expiry).

```sh
freedom publish mysite --api http://localhost:8420
```

```
Published mysite.<pubKeyID>.fn (seq 1720713600, 2 record(s))
```

Fails if there are no staged records, or if the node rejects the record (e.g. it
fails verification).

## `freedom put <label> <file> [--api URL] [--ttl S]`

The one-step author flow: uploads a file's bytes to a running node, points
`<label>` at the resulting content hash (a single `CONTENT` record), and
publishes. This is what a browser's editor triggers when a user saves a page.

```sh
freedom keygen blog
freedom put blog ./index.html
```

```
Uploaded ./index.html (2048 bytes) -> muf...hbst
Published blog.<pubKeyID>.fn (seq ..., 1 record(s))
```

Now `blog.<pubKeyID>.fn` resolves to the page: fetch it with
`GET /resolve-content?name=blog.<pubKeyID>.fn`. See
[the content network](/guide/content).

::: warning
Unlike `freedom set`, which merges into the staged set, `put` **replaces** all
staged records for `<label>` with the single `CONTENT` record it publishes. Use
`set` + `publish` if the name should carry other records alongside its content.
:::

## `freedom lookup <name> [--api URL] [--type TYPE]`

Resolves a full name via a node's `/resolve` endpoint and prints the JSON
response. Optionally filter by record type.

```sh
freedom lookup mysite.<pubKeyID>.fn
freedom lookup mysite.<pubKeyID>.fn --type A
```

## Bare names on Bitcoin Cash

These commands register globally-unique bare names (`mysite.fn`, no key suffix)
on Bitcoin Cash. They talk directly to an Electrum server and do **not** need a
running node. They default to **mainnet**; set `FREEDOM_BCH_NETWORK=chipnet` (or
`testnet4` / `testnet3`) to rehearse for free with faucet coins. Servers come
from a built-in per-network list with failover unless you override
`FREEDOM_BCH_ELECTRUM`. See [Bare names](/guide/bare-names) for the full protocol.

### `freedom wallet`

Shows the BCH funding address (a `bitcoincash:` address on mainnet, or a
`bchtest:` one you can fund from a faucet on a test network), balance, and how
many name NFTs the wallet holds.

### `freedom claim <label>`

Registers `<label>.fn` on-chain: mints the name NFT bound to the label's Ed25519
owner key (`freedom keygen <label>` first), and prints the transaction id. First
confirmed claim wins.

### `freedom adopt <label>`

Re-binds a name NFT you received by a plain wallet transfer to your own key, so
`<label>.fn` resolves to your records.

### `freedom whois <name>`

Shows the on-chain owner of a bare name, including the equivalent
self-certifying `<label>.<pubKeyID>.fn` name.

## Where things live

| Path | Contents |
| --- | --- |
| `~/.freedom/keys/<label>.key` | the owner private key for a name |
| `~/.freedom/keys/<label>.records.json` | staged records awaiting publish |
| `~/.freedom/bch.key` | the BCH wallet key (funds bare-name claims) |

The node's own libp2p identity (`private.key`) is **separate**, so your names are
portable between nodes.

## Next

- The [**HTTP API**](/guide/http-api) the CLI talks to.
- A full walkthrough: [**your first name**](/guide/your-first-name).
