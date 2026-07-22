# Rotate your records

Changing what a name points at is just publishing a new signed record. The network
keeps the one with the **highest sequence number**, and the CLI derives that number
from wall-clock time, always strictly above the name's current record, so a later
publish wins even twice within the same second or after a backward clock step.

## Change an IP address

Say `blog` moved to a new server. Clear the old staged set, stage the new one, and
publish:

```sh
./freedom-names freedom clear blog
./freedom-names freedom set blog A 198.51.100.9 300
./freedom-names freedom publish blog
# Published blog.<pubKeyID>.fn (seq …, 1 record(s))
```

The new record's sequence number is larger than the previous publish, so it
supersedes it across the network.

::: tip Why clear first?
`freedom set` **appends** each new type+value pair to whatever is staged locally
(re-setting an identical pair just updates its TTL). If you don't
`freedom clear`, you'll publish both the old and new records. Clear to replace;
skip clearing to add.
:::

## See the update immediately

Nodes cache resolutions, so right after publishing you may still see the cached
answer. Clear a node's cache to force a fresh DHT read:

```sh
curl -X DELETE http://localhost:8420/clear_cache
./freedom-names freedom lookup blog.<pubKeyID>.fn --type A
```

## How "newest wins" is enforced

- Every record carries a `seq`; a tie falls to the later `eol` expiry, then to
  the larger raw record bytes.
- The DHT **validator** on every node selects the record with the higher `seq`.
- A competing update is only accepted if it carries a **valid signature from the
  same key**, so an attacker can't win the race without your private key.

That's the whole update model: no registrar to notify, no propagation delay through
a hierarchy, just a newer signed record.

## Rotating the key itself

Rotating the *keypair* behind a self-certifying name would change `<pubKeyID>`, and
therefore the name, so it's effectively a new name. Keeping the **same human name**
while changing the underlying key is a registry feature (the covenant **transfer**
operation for bare names). See [Bare names](/guide/bare-names).
