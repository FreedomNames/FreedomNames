# Rotate your records

Changing what a name points at is just publishing a new signed record. The network
keeps the one with the **highest sequence number**, and the CLI derives that number
from wall-clock time — so a later publish always wins.

## Change an IP address

Say `blog` moved to a new server. Clear the old staged set, stage the new one, and
publish:

```sh
freedom clear blog
freedom set blog A 198.51.100.9 300
freedom publish blog
# Published blog.<pubKeyID>.fn (seq …, 1 record(s))
```

The new record's sequence number is larger than the previous publish, so it
supersedes it across the network.

::: tip Why clear first?
`freedom set` **appends** to whatever is staged locally. If you don't
`freedom clear`, you'll publish both the old and new records. Clear to replace;
skip clearing to add.
:::

## See the update immediately

Nodes cache resolutions, so right after publishing you may still see the cached
answer. Clear a node's cache to force a fresh DHT read:

```sh
curl -X DELETE http://localhost:8080/clear_cache
freedom lookup blog.<pubKeyID>.fn --type A
```

## How "newest wins" is enforced

- Every record carries a `seq` (and an `eol` expiry used as a tiebreaker).
- The DHT **validator** on every node selects the record with the higher `seq`.
- A competing update is only accepted if it carries a **valid signature from the
  same key** — so an attacker can't win the race without your private key.

That's the whole update model: no registrar to notify, no propagation delay through
a hierarchy — just a newer signed record.

## Rotating the key itself

Rotating the *keypair* behind a self-certifying name would change `<pubKeyID>`, and
therefore the name — so it's effectively a new name. Keeping the **same human name**
while changing the underlying key is a Layer 2 feature (the covenant **transfer**
operation for bare names). See [Layer 2](/guide/layer2).
