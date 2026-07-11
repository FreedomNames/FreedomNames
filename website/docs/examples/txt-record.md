# Publish a TXT record

`TXT` records hold arbitrary UTF-8 text under a name, useful for verification
tokens, SPF policies, public notes, or small machine-readable metadata.

## Stage and publish

```sh
freedom keygen notes            # skip if the name already exists
freedom set notes TXT "hello from freedom names"
freedom set notes TXT "v=spf1 -all"
freedom publish notes
```

You can stage **multiple** `TXT` records; they're all published together under the
one name.

## Read it back

Filter the resolve to just `TXT`:

```sh
freedom lookup notes.<pubKeyID>.fn --type TXT
```

Or over DNS:

```sh
dig @127.0.0.1 -p 8053 notes.<pubKeyID>.fn TXT
```

Or straight over the HTTP API:

```sh
curl "http://localhost:8080/resolve?name=notes.<pubKeyID>.fn&type=TXT"
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
freedom set notes TXT "this is one value"
```

## Replace vs. append

`freedom set` **appends** to the staged set. To publish a fresh set, clear first:

```sh
freedom clear notes
freedom set notes TXT "only this now"
freedom publish notes
```

The new publish supersedes the old record on the network (higher sequence number).
See [Rotate your records](/examples/rotate-records) for the details.
