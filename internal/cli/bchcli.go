package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/bch"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/config"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

// This file holds the BCH bare-name registry CLI subcommands:
// wallet, claim, adopt, whois. They talk directly to an Electrum server
// configured via FREEDOM_BCH_* (see internal/config) — they do not need a running
// freedom node.

// bchContext returns a context bounded for interactive CLI chain operations.
func bchContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 45*time.Second)
}

// bchWalletFromEnv opens the wallet against the configured electrum server.
func bchWalletFromEnv() (*bch.Wallet, *bch.ElectrumClient, error) {
	cfg := config.LoadConfig()
	if len(cfg.BCHElectrum) == 0 {
		return nil, nil, fmt.Errorf("no BCH electrum server configured (set FREEDOM_BCH_ELECTRUM or FREEDOM_BCH_NETWORK)")
	}
	client := bch.NewElectrumClient(cfg.BCHElectrum...)
	w, err := bch.LoadOrCreateWallet(cfg.BCHNetwork, client)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return w, client, nil
}

func cliWallet(args []string) error {
	w, client, err := bchWalletFromEnv()
	if err != nil {
		return err
	}
	defer client.Close()

	// Print the address first: it is useful even when the server is
	// unreachable (you need it to fund the wallet before any balance exists).
	fmt.Printf("BCH wallet (%s)\n", w.Network())
	fmt.Printf("  Address: %s\n", w.Address())

	ctx, cancel := bchContext()
	defer cancel()

	balance, nfts, err := w.Holdings(ctx)
	if err != nil {
		fmt.Printf("  Balance: unavailable (%v)\n", err)
		return nil
	}

	fmt.Printf("  Balance: %d sat (%.8f BCH)\n", balance, float64(balance)/1e8)
	fmt.Printf("  Name NFTs held: %d\n", nfts)
	if balance == 0 && w.Network() == "chipnet" {
		fmt.Println("  Fund this address from a chipnet faucet, then run: freedom claim <name>")
	}
	return nil
}

func cliClaim(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: freedom claim <label>")
	}
	label, err := bch.NormalizeName(args[0])
	if err != nil {
		return err
	}

	// The name is bound to the label's existing Ed25519 owner key.
	ownerPriv, err := loadKey(label)
	if err != nil {
		return fmt.Errorf("%w\n(run: freedom keygen %s first)", err, label)
	}
	ownerPub, err := crypto.MarshalPublicKey(ownerPriv.GetPublic())
	if err != nil {
		return err
	}

	w, client, err := bchWalletFromEnv()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := bchContext()
	defer cancel()

	// Refuse to double-claim if the name already resolves to someone.
	reg := bch.NewBCHRegistry(client, config.LoadConfig().BCHMinConf)
	if existing, err := reg.ResolveOwner(label + "." + record.TLD); err == nil {
		if bytes.Equal(existing, ownerPub) {
			return fmt.Errorf("%q is already claimed by this key", label)
		}
		return fmt.Errorf("%q is already claimed by another owner", label)
	}

	raw, err := w.BuildClaimTx(ctx, label, ownerPub)
	if err != nil {
		return err
	}
	txid, err := client.Broadcast(ctx, raw)
	if err != nil {
		return fmt.Errorf("broadcast claim: %w", err)
	}

	fmt.Printf("Claimed %q on %s\n", label, w.Network())
	fmt.Printf("  tx: %s\n", txid)
	fmt.Printf("  Once confirmed, %s.fn resolves to this name's records.\n", label)
	return nil
}

func cliAdopt(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: freedom adopt <label>")
	}
	label, err := bch.NormalizeName(args[0])
	if err != nil {
		return err
	}
	ownerPriv, err := loadKey(label)
	if err != nil {
		return fmt.Errorf("%w\n(run: freedom keygen %s first)", err, label)
	}
	ownerPub, err := crypto.MarshalPublicKey(ownerPriv.GetPublic())
	if err != nil {
		return err
	}

	w, client, err := bchWalletFromEnv()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := bchContext()
	defer cancel()

	// The category is the earliest confirmed claim's txid.
	reg := bch.NewBCHRegistry(client, config.LoadConfig().BCHMinConf)
	category, err := reg.CategoryFor(ctx, label)
	if err != nil {
		return fmt.Errorf("find name NFT category: %w", err)
	}

	raw, err := w.BuildRebindTx(ctx, label, category, ownerPub)
	if err != nil {
		return err
	}
	txid, err := client.Broadcast(ctx, raw)
	if err != nil {
		return fmt.Errorf("broadcast rebind: %w", err)
	}
	fmt.Printf("Adopted %q -> your key\n  tx: %s\n", label, txid)
	return nil
}

func cliWhois(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: freedom whois <name>")
	}
	label, err := bch.NormalizeName(args[0])
	if err != nil {
		return err
	}

	cfg := config.LoadConfig()
	if len(cfg.BCHElectrum) == 0 {
		return fmt.Errorf("no BCH electrum server configured (set FREEDOM_BCH_ELECTRUM or FREEDOM_BCH_NETWORK)")
	}
	client := bch.NewElectrumClient(cfg.BCHElectrum...)
	defer client.Close()

	reg := bch.NewBCHRegistry(client, cfg.BCHMinConf)
	ownerPub, err := reg.ResolveOwner(label + "." + record.TLD)
	if err != nil {
		return fmt.Errorf("%q: %w", label, err)
	}
	id, err := record.PubKeyID(ownerPub)
	if err != nil {
		return err
	}
	fmt.Printf("%s.fn\n", label)
	fmt.Printf("  owner pubkey id: %s\n", id)
	fmt.Printf("  self-certifying: %s.%s.%s\n", label, id, record.TLD)
	fmt.Printf("  owner pubkey:    %s\n", hex.EncodeToString(ownerPub))
	return nil
}
