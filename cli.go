package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
)

// defaultAPI is the node HTTP API the CLI talks to by default. It matches the
// node's default FREEDOM_HTTP_ADDR (":8420") so `publish`/`lookup` work out of
// the box against a locally running node.
const defaultAPI = "http://localhost:8420"

// cliUsage documents the freedom subcommands.
const cliUsage = `freedom - manage Freedom Names

Usage:
  freedom keygen <label>                 Generate an owner keypair for a name
  freedom set <label> <TYPE> <VALUE> [ttl]   Stage a resource record (A|AAAA|TXT|CNAME)
  freedom clear <label>                  Remove all staged records for a name
  freedom publish <label> [--api URL]    Sign staged records and publish to a running node
  freedom name <label>                   Print the full "label.<pubKeyID>.fn" name
  freedom lookup <name> [--api URL] [--type TYPE]   Resolve a name via a running node

Bare names on Bitcoin Cash (Layer 2, set FREEDOM_BCH_ELECTRUM):
  freedom wallet                         Show the BCH funding address + balance
  freedom claim <label>                  Register the bare "<label>.fn" name on-chain
  freedom adopt <label>                  Re-bind a name NFT you received to your key
  freedom whois <name>                   Show the on-chain owner of a bare name

Keys and staged records live under ~/.freedom/keys/; the BCH wallet key in
~/.freedom/bch.key. The default node API is http://localhost:8420 (--api).
`

// RunCLI dispatches a "freedom" subcommand.
func RunCLI(args []string) {
	if len(args) == 0 {
		fmt.Print(cliUsage)
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "keygen":
		err = cliKeygen(args[1:])
	case "set":
		err = cliSet(args[1:])
	case "clear":
		err = cliClear(args[1:])
	case "publish":
		err = cliPublish(args[1:])
	case "name":
		err = cliName(args[1:])
	case "lookup":
		err = cliLookup(args[1:])
	case "wallet":
		err = cliWallet(args[1:])
	case "claim":
		err = cliClaim(args[1:])
	case "adopt":
		err = cliAdopt(args[1:])
	case "whois":
		err = cliWhois(args[1:])
	case "help", "-h", "--help":
		fmt.Print(cliUsage)
		return
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", args[0], cliUsage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// keysDir returns ~/.freedom/keys, creating it if needed.
func keysDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".freedom", "keys")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func keyPath(label string) (string, error) {
	dir, err := keysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, label+".key"), nil
}

func stagePath(label string) (string, error) {
	dir, err := keysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, label+".records.json"), nil
}

// loadKey loads the owner private key for a label.
func loadKey(label string) (crypto.PrivKey, error) {
	path, err := keyPath(label)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no key for %q (run: freedom keygen %s): %w", label, label, err)
	}
	return crypto.UnmarshalPrivateKey(data)
}

func cliKeygen(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: freedom keygen <label>")
	}
	label := args[0]
	path, err := keyPath(label)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("key for %q already exists at %s", label, path)
	}

	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, rand.Reader)
	if err != nil {
		return err
	}
	data, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	pub, _ := crypto.MarshalPublicKey(priv.GetPublic())
	id, _ := pubKeyID(pub)
	fmt.Printf("Generated key for %q\n", label)
	fmt.Printf("Your name: %s.%s.%s\n", label, id, tld)
	return nil
}

func cliSet(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: freedom set <label> <TYPE> <VALUE> [ttl]")
	}
	label, rtype, value := args[0], args[1], args[2]
	ttl := uint32(300)
	if len(args) >= 4 {
		var parsed uint64
		if _, err := fmt.Sscanf(args[3], "%d", &parsed); err != nil {
			return fmt.Errorf("invalid ttl %q: %w", args[3], err)
		}
		ttl = uint32(parsed)
	}

	records, err := loadStaged(label)
	if err != nil {
		return err
	}

	// Dedupe by (type, value): re-setting the same record updates its TTL in
	// place rather than staging a duplicate.
	newRR := RR{Type: rtype, Value: value, TTL: ttl}
	updated := false
	for i := range records {
		if records[i].Type == newRR.Type && records[i].Value == newRR.Value {
			records[i].TTL = ttl
			updated = true
			break
		}
	}
	if !updated {
		records = append(records, newRR)
	}

	// Validate the whole set eagerly so mistakes surface now, not at publish.
	tmp := &FNRecord{Label: label, Records: records}
	if err := tmp.validateRecords(); err != nil {
		return err
	}
	if err := saveStaged(label, records); err != nil {
		return err
	}

	action := "Staged"
	if updated {
		action = "Updated"
	}
	fmt.Printf("%s %s %s (ttl %d) for %q (%d record(s) staged)\n", action, rtype, value, ttl, label, len(records))
	return nil
}

func cliClear(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: freedom clear <label>")
	}
	path, err := stagePath(args[0])
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("Cleared staged records for %q\n", args[0])
	return nil
}

func cliName(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: freedom name <label>")
	}
	priv, err := loadKey(args[0])
	if err != nil {
		return err
	}
	pub, _ := crypto.MarshalPublicKey(priv.GetPublic())
	id, err := pubKeyID(pub)
	if err != nil {
		return err
	}
	fmt.Printf("%s.%s.%s\n", args[0], id, tld)
	return nil
}

func cliPublish(args []string) error {
	label, flags := popPositional(args)
	if label == "" {
		return fmt.Errorf("usage: freedom publish <label> [--api URL]")
	}
	api := flagValue(flags, "--api", defaultAPI)

	priv, err := loadKey(label)
	if err != nil {
		return err
	}
	records, err := loadStaged(label)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("no staged records for %q (use: freedom set ...)", label)
	}

	// Sequence numbers must increase per name for updates to win in the DHT.
	var current *FNRecord
	pub, _ := crypto.MarshalPublicKey(priv.GetPublic())
	if id, idErr := pubKeyID(pub); idErr == nil {
		fullName := label + "." + id + "." + tld
		if rec, ok := fetchCurrentRecord(api, fullName); ok {
			current = rec
		}
	}
	seq := nextSeq(uint64(time.Now().Unix()), current)
	rec, err := BuildAndSignRecord(priv, label, records, seq)
	if err != nil {
		return err
	}
	payload, err := rec.Marshal()
	if err != nil {
		return err
	}

	resp, err := http.Post(api+"/publish", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("publish to %s: %w", api, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node rejected publish (%d): %s", resp.StatusCode, string(body))
	}
	name, _ := rec.FullName()
	fmt.Printf("Published %s (seq %d, %d record(s))\n", name, seq, len(records))
	fmt.Printf("Record valid until %s. Re-run publish before then to renew.\n",
		time.Unix(rec.EOL, 0).Format(time.RFC1123))
	return nil
}

func cliLookup(args []string) error {
	name, flags := popPositional(args)
	if name == "" {
		return fmt.Errorf("usage: freedom lookup <name> [--api URL] [--type TYPE]")
	}
	api := flagValue(flags, "--api", defaultAPI)
	rtype := flagValue(flags, "--type", "")

	// Escape query values so metacharacters in a name can't corrupt the URL.
	params := url.Values{"name": {name}}
	if rtype != "" {
		params.Set("type", rtype)
	}
	resp, err := http.Get(api + "/resolve?" + params.Encode())
	if err != nil {
		return fmt.Errorf("lookup via %s: %w", api, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lookup failed (%d): %s", resp.StatusCode, string(body))
	}
	fmt.Println(string(body))
	return nil
}

// nextSeq picks the sequence number for a publish: wall-clock time, but always
// strictly above the name's current record when one exists. This keeps updates
// winning in the DHT even for same-second double publishes or a clock stepped
// backwards, either of which would otherwise wedge updates. Saturates at
// MaxUint64 rather than wrapping to 0 on a (hostile) maximal current record.
func nextSeq(wallClock uint64, current *FNRecord) uint64 {
	if current != nil && current.Seq >= wallClock {
		if current.Seq == math.MaxUint64 {
			return math.MaxUint64
		}
		return current.Seq + 1
	}
	return wallClock
}

// fetchCurrentRecord fetches the current signed record for a full name from a
// running node, returning ok=false if the name has no record or the node is
// unreachable (first publish, or transient failure; callers fall back).
func fetchCurrentRecord(api, fullName string) (*FNRecord, bool) {
	params := url.Values{"name": {fullName}}
	resp, err := http.Get(api + "/record?" + params.Encode())
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	rec, err := UnmarshalFNRecord(body)
	if err != nil {
		return nil, false
	}
	return rec, true
}

// --- staged records helpers ---

func loadStaged(label string) ([]RR, error) {
	path, err := stagePath(label)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []RR
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func saveStaged(label string, records []RR) error {
	path, err := stagePath(label)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// --- tiny flag helpers (avoids a flag dependency for the mixed positional/flag CLI) ---

// popPositional returns the first non-flag arg and the rest.
func popPositional(args []string) (string, []string) {
	for i, a := range args {
		if len(a) >= 2 && a[:2] == "--" {
			continue
		}
		rest := append([]string{}, args[:i]...)
		rest = append(rest, args[i+1:]...)
		return a, rest
	}
	return "", args
}

// flagValue returns the value following --name, or fallback.
func flagValue(args []string, name, fallback string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return fallback
}
