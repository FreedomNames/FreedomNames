# Publish a TXT record

`TXT` records hold arbitrary UTF-8 text under a name, useful for verification
tokens, SPF policies, public notes, or small machine-readable metadata.

## Stage and publish

```sh
./freedom-names freedom keygen notes  # skip if the name already exists
./freedom-names freedom set notes TXT "hello from freedom names"
./freedom-names freedom set notes TXT "v=spf1 -all"
./freedom-names freedom publish notes
```

You can stage **multiple** `TXT` records; they're all published together under the
one name.

Each value is capped at **255 bytes**, the DNS character-string limit — a longer
one cannot be put on the wire, so `set` rejects it rather than letting you
publish a record no resolver could answer with. Split a longer payload across
several `TXT` records, or store it as [content](/guide/content) and point a
`CONTENT` record at it. A name carries at most 32 records in total.

## Read it back

Filter the resolve to just `TXT`:

```sh
./freedom-names freedom lookup notes.<pubKeyID>.fn --type TXT
```

Or over DNS:

```sh
dig @127.0.0.1 -p 8053 notes.<pubKeyID>.fn TXT
```

Or straight over the HTTP API:

```sh
curl "http://localhost:8420/resolve?name=notes.<pubKeyID>.fn&type=TXT"
```

```json
{
  "name": "notes.<pubKeyID>.fn",
  "records": [
    { "type": "TXT", "value": "hello from freedom names", "ttl": 300 },
    { "type": "TXT", "value": "v=spf1 -all", "ttl": 300 }
  ]
}
```

## Quote values with spaces

Wrap any value containing spaces in quotes so your shell passes it as a single
argument:

```sh
./freedom-names freedom set notes TXT "this is one value"
```

## Replace vs. append

`freedom set` **appends** each new type+value pair to the staged set; re-setting
an identical pair just updates its TTL. To publish a fresh set, clear first:

```sh
./freedom-names freedom clear notes
./freedom-names freedom set notes TXT "only this now"
./freedom-names freedom publish notes
```

The new publish supersedes the old record on the network (higher sequence number).
See [Rotate your records](/examples/rotate-records) for the details.
