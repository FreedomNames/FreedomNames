# Claiming a bare name on chipnet

This is a full, reproducible walkthrough for registering a globally-unique bare
name (`mysite.fn`) on Bitcoin Cash **chipnet** (BCH's test network) and resolving
it end to end. It costs nothing real: chipnet coins come free from a faucet.

By the end you will have minted a real CashTokens NFT that *is* your name, and
watched `mysite.fn` resolve through the chain.

## 1. Download Freedom Names

Download and extract the prebuilt package for your platform by following [Run
Freedom Names](/guide/running-a-node#download-a-release).

## 2. Point at chipnet

Bare names default to **mainnet**, where a claim costs real BCH. This
walkthrough uses **chipnet** so it costs nothing, so switch networks for the
session:

```sh
export FREEDOM_BCH_NETWORK=chipnet
```

That is all you need: the node uses a built-in list of public chipnet
Electrum/Fulcrum servers automatically, failing over between them. To use a
specific server (for example your own), set
`FREEDOM_BCH_ELECTRUM=ssl://host:port`.

## 3. Create your funding wallet

```sh
./freedom-names freedom wallet
```

```
BCH wallet (chipnet)
  Address: bchtest:qq....
  Balance: 0 sat (0.00000000 BCH)
  Name NFTs held: 0
  Fund this address from a chipnet faucet, then run: freedom claim <name>
```

Copy the `bchtest:...` address. The wallet key lives in `~/.freedom/bch.key`.

## 4. Fund it from a chipnet faucet

Open a chipnet faucet and send a small amount to your `bchtest:` address. A few
thousand satoshis is plenty: a claim locks 1000 sat in the name NFT (which stays
in your own wallet), pays a 546-sat discovery marker, plus a tiny fee. Chipnet faucets to try:

- https://tbch.googol.cash/ (select chipnet)
- Search "BCH chipnet faucet" for current options; chipnet faucets rotate.

Wait for one confirmation, then check:

```sh
./freedom-names freedom wallet
```

You should now see a non-zero balance.

::: tip One coin at output index 0
Minting a token requires spending a coin whose output index is `0` (the
CashTokens genesis rule). A faucet payment is usually a single output, so it
qualifies. If `claim` ever reports "no eligible genesis UTXO", just send yourself
a little BCH (or request another faucet payment) and retry.
:::

## 5. Create the owner key for the name

The bare name is bound to a normal Freedom Names owner key (the same kind that
self-certifying names use), so records work identically:

```sh
./freedom-names freedom keygen mysite
```

## 6. Claim the name

```sh
./freedom-names freedom claim mysite
```

```
Claimed "mysite" on chipnet
  tx: <txid>
  Once confirmed, mysite.fn resolves to this name's records.
```

This one transaction mints a mutable NFT (its commitment is the hash of your
owner key), reveals your key in an `FN01` OP_RETURN, and pays the discovery
marker. Look the `<txid>` up on a chipnet explorer that understands CashTokens,
e.g. https://chipnet.imaginary.cash/: you should see a newly minted NFT.

## 7. Resolve it

After one confirmation:

```sh
./freedom-names freedom whois mysite.fn
```

```
mysite.fn
  owner pubkey id: <pubKeyID>
  self-certifying: mysite.<pubKeyID>.fn
  owner pubkey:    <hex>
```

`whois` read the chain, found the earliest confirmed claim, walked the NFT to its
current holder, and returned your owner key. The `self-certifying` line is the
equivalent self-certifying name that always points at the same records.

## 8. Publish records and resolve over DNS

Bare names resolve to the same records as their self-certifying twin. Publish
some, then resolve the bare name through a running node:

```sh
# in one terminal: run a node (DNS on :8053, HTTP on :8420)
./freedom-names

# in another terminal:
./freedom-names freedom set mysite A 203.0.113.10
./freedom-names freedom publish mysite
./freedom-names freedom lookup mysite.fn          # via the node's HTTP API
dig @127.0.0.1 -p 8053 mysite.fn A                # via DNS
```

The node consults the BCH registry for the bare `mysite.fn`, gets your owner key,
then resolves the signed records from the DHT, exactly as for the self-certifying
name.

## Transferring a name

A name is an ordinary CashTokens NFT, so you can send it in any token-aware
chipnet wallet (e.g. Cashonize). The receiver then binds it to their own key:

```sh
./freedom-names freedom keygen mysite     # receiver's own owner key
./freedom-names freedom adopt mysite      # spends the NFT, rebinds it
```

`adopt` only works once the NFT is actually held by *this* wallet (the key in
`~/.freedom/bch.key`); until the transfer confirms there, it fails with
`this wallet does not hold the "mysite" name NFT`. Note that until the receiver
runs `adopt`, the name still resolves to the previous owner's records.

## Troubleshooting

- **`no BCH electrum server configured`**: `FREEDOM_BCH_NETWORK` is set to an
  unknown value (so there is no built-in list). Use `mainnet`, `chipnet`,
  `testnet4`, or `testnet3`, or set `FREEDOM_BCH_ELECTRUM` explicitly.
- **`insufficient funds`**: the faucet payment has not confirmed yet, or was too
  small; check `freedom wallet`.
- **`no eligible genesis UTXO`**: send yourself a little BCH so you have a plain
  coin at output index 0, then retry (see the tip in step 4).
- **`"mysite" is already claimed`**: someone (maybe you, earlier) already
  registered it; the first confirmed claim wins. Pick another label.
- **`whois` says not found right after claiming**: wait for one confirmation; by
  default a claim must be confirmed (`FREEDOM_BCH_MINCONF`, default 1) to count.
  A not-found answer is also cached for 30 seconds, so retry shortly after the
  confirmation lands.
- **`all N electrum endpoints failed`**: every server in the list is currently
  unreachable. This is transient — the failure is not cached, so the next
  lookup retries the whole list. `freedom wallet` still prints your address but
  shows `Balance: unavailable`.
- **`adopt` says `this wallet does not hold the ... name NFT`**: the NFT has not
  (yet) arrived in this machine's wallet. Send it to the address shown by
  `freedom wallet` and wait for a confirmation first.
