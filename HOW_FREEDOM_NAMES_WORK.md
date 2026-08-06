# How Freedom Names work

A name looks like:

```
mysite.<pubKeyID>.fn
```

where `<pubKeyID>` is the base36 hash of the owner's public key (self-certifying,
like IPNS/GNS). Because the key *is* the name, squatting is impossible: no ledger
required. Records are signed `FNRecord`s stored in the DHT under a key derived
from the owner's public key; the validator verifies the signature and the
key→name binding before accepting any update, and the newest record (highest
sequence number) wins.

Globally-unique *bare* names (`mysite.fn`, no key suffix) are handled by
the **name registry**: a claimed name is a CashTokens NFT on Bitcoin Cash, and its
uniqueness is enforced by BCH chain consensus. Resolvers all agree because they
follow the same on-chain rule: the earliest *confirmed* valid claim wins (ties
broken by smaller txid), and ownership can only move by a transaction that
actually spends the name's NFT UTXO, so metadata-only hijacks are rejected.

End to end, resolving a name (say `melroy.fn`) to page bytes looks like this —
note that no step ever addresses an IP or a server; the name commits to a key,
the key signs records, and the content's *hash* is its address:

```plantuml
@startuml
skinparam ranksep 25
skinparam nodesep 14
skinparam defaultFontSize 12
skinparam shadowing false
skinparam ArrowColor #37474F
skinparam ArrowFontColor #37474F
skinparam activity {
  BorderColor #FFFFFF
  FontColor #FFFFFF
  DiamondBackgroundColor #FFC94D
  DiamondBorderColor #E09B00
  DiamondFontColor #3D2C00
}
skinparam partition {
  BorderColor #90A4AE
  FontColor #37474F
}

start
:Open <b>melroy.fn</b>; <<#37474F>>
if (Name carries a <pubKeyID> suffix?) then (no — bare name)
  partition "Bare names — BCH registry" {
    :Find the earliest confirmed claim: the name is a CashTokens NFT; <<#12805F>>
    :Walk the NFT's custody chain to its current UTXO; <<#12805F>>
    :Live token commitment reveals the owner's public key; <<#12805F>>
  }
else (yes — self-certifying)
  :Owner's public key is embedded in the name itself; <<#6D28D9>>
endif
partition "Self-certifying — DHT (naming)" {
  :Derive the DHT key from the pubKeyID; <<#6D28D9>>
  :Fetch the signed record set (newest sequence wins); <<#6D28D9>>
  :Verify the signature against the owner's public key; <<#6D28D9>>
  :Read the CONTENT record → content hash; <<#6D28D9>>
}
partition "Content network (bytes)" {
  if (Blob in the local store?) then (yes)
  else (no)
    :Ask the DHT who provides the hash (publisher + pushed replicas); <<#1D6FBF>>
    :Stream the blob from any provider; <<#1D6FBF>>
  endif
  :Verify the bytes against the hash (wrong content is impossible); <<#1D6FBF>>
  if (Blob is a chunk manifest?) then (yes)
    :Fetch each chunk the same way, reassemble as a stream; <<#1D6FBF>>
  else (no)
  endif
}
:Render the page bytes; <<#37474F>>
stop
@enduml
```

Bare names default to BCH **mainnet**, since they are a real,
globally-unique namespace. A node reaches the chain through public
Electrum/Fulcrum servers: it ships with a built-in bootstrap list per network
and **fails over** between them, so no single server is a point of failure.
Self-certifying names work without any of this.

To experiment first with free coins, point the registry at a test network:

```sh
# chipnet (fast test network, faucet coins)
FREEDOM_BCH_NETWORK=chipnet ./freedom-names

# testnet4
FREEDOM_BCH_NETWORK=testnet4 ./freedom-names
```
