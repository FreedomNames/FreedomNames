package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// electrumClient is a minimal Electrum Cash protocol client (the protocol
// served by Fulcrum): newline-delimited JSON-RPC 2.0 over TCP or TLS. It is
// deliberately tiny: only the handful of methods the BCH registry and wallet
// need, instead of pulling in a heavy chain library.
//
// Endpoints use an explicit scheme: "ssl://host:port" or "tcp://host:port"
// (the Electrum convention; public servers usually speak ssl on :50002).
//
// A client holds a list of endpoints and fails over between them: on connect it
// tries each in turn until one is reachable and speaks the protocol, so a single
// dead public server never takes the registry down. In the spirit of a decentralized
// network, the default lists (config.go) carry several independent operators.
type electrumClient struct {
	endpoints []string

	mu     sync.Mutex // guards conn/reader/nextID/lastGood; one in-flight call at a time
	conn   net.Conn
	reader *bufio.Reader
	nextID uint64

	// lastGood is the index of the endpoint that last connected successfully;
	// the next connect starts there so we stick to a working server instead of
	// re-probing dead ones from the top every time.
	lastGood int
}

// electrumDialTimeout bounds connection establishment to a single server.
const electrumDialTimeout = 15 * time.Second

// newElectrumClient creates a client over the given endpoints, tried in order
// with failover. At least one endpoint is required. The connection is
// established lazily on first call and re-established after errors.
func newElectrumClient(endpoints ...string) *electrumClient {
	return &electrumClient{endpoints: endpoints}
}

// connect (re)establishes the connection, trying each endpoint in turn until one
// works. It starts from the last-known-good endpoint so a healthy server is
// reused across reconnects. Callers must hold c.mu.
func (c *electrumClient) connect(ctx context.Context) error {
	if len(c.endpoints) == 0 {
		return errors.New("no electrum endpoints configured")
	}
	var errs []error
	n := len(c.endpoints)
	for off := 0; off < n; off++ {
		i := (c.lastGood + off) % n
		endpoint := c.endpoints[i]
		if err := c.dial(ctx, endpoint); err != nil {
			errs = append(errs, err)
			continue
		}
		c.lastGood = i
		return nil
	}
	return fmt.Errorf("all %d electrum endpoints failed: %w", n, errors.Join(errs...))
}

// dial connects to a single endpoint and negotiates the protocol version. On
// success c.conn/c.reader are set; on failure they are left nil. Callers must
// hold c.mu.
func (c *electrumClient) dial(ctx context.Context, endpoint string) error {
	scheme, addr, ok := strings.Cut(endpoint, "://")
	if !ok {
		// Default to ssl, the common public-server transport.
		scheme, addr = "ssl", endpoint
	}

	dialer := &net.Dialer{Timeout: electrumDialTimeout}
	var (
		conn net.Conn
		err  error
	)
	switch scheme {
	case "ssl", "tls":
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			// Public electrum servers overwhelmingly use self-signed certs;
			// the protocol's trust model is "pick servers you trust", so
			// certificate verification is intentionally disabled (matching
			// Electron Cash and every other electrum client).
			InsecureSkipVerify: true,
		})
	case "tcp":
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	default:
		return fmt.Errorf("electrum endpoint %q: unknown scheme %q (use ssl:// or tcp://)", endpoint, scheme)
	}
	if err != nil {
		return fmt.Errorf("connect to electrum server %s: %w", endpoint, err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)

	// Negotiate a protocol version that includes CashTokens data (>= 1.5.0).
	var version []string
	if err := c.callLocked(ctx, "server.version", []any{"freedom-names", []string{"1.4", "1.5.3"}}, &version); err != nil {
		c.closeLocked()
		return fmt.Errorf("electrum version negotiation with %s: %w", endpoint, err)
	}
	return nil
}

func (c *electrumClient) closeLocked() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.reader = nil
	}
}

// Close shuts the connection down.
func (c *electrumClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

// call performs one JSON-RPC request/response cycle, connecting if needed. On
// any transport error the connection is dropped so the next call reconnects.
func (c *electrumClient) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		if err := c.connect(ctx); err != nil {
			return err
		}
	}
	err := c.callLocked(ctx, method, params, result)
	if err != nil && !isElectrumRPCError(err) {
		// Transport-level failure: force a reconnect on the next call.
		c.closeLocked()
	}
	return err
}

// electrumRPCError is an error returned by the server (as opposed to a
// transport failure, which warrants a reconnect).
type electrumRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *electrumRPCError) Error() string {
	return fmt.Sprintf("electrum rpc error %d: %s", e.Code, e.Message)
}

func isElectrumRPCError(err error) bool {
	var rpcErr *electrumRPCError
	return errors.As(err, &rpcErr)
}

// callLocked sends one request and reads the matching response. Callers must
// hold c.mu and have a live connection.
func (c *electrumClient) callLocked(ctx context.Context, method string, params any, result any) error {
	c.nextID++
	id := c.nextID

	// Normalize nil to an empty array: some Fulcrum builds reject "params": null
	// with -32600 Invalid params, but accept an empty list.
	if params == nil {
		params = []any{}
	}
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}

	// Respect the context deadline on the socket for both write and read.
	deadline := time.Now().Add(electrumDialTimeout)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	c.conn.SetDeadline(deadline)

	if _, err := c.conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("electrum write: %w", err)
	}

	// Read lines until our response id shows up, skipping server
	// notifications (which have no id or a different one).
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("electrum read: %w", err)
		}
		var resp struct {
			ID     uint64            `json:"id"`
			Result json.RawMessage   `json:"result"`
			Error  *electrumRPCError `json:"error"`
		}
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // notification or garbage; keep reading
		}
		if resp.ID != id {
			continue // notification/stale response
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, result)
	}
}

// --- typed method wrappers ---

// electrumHistoryItem is one entry of blockchain.scripthash.get_history.
// Height 0 means unconfirmed; -1 unconfirmed with unconfirmed parents.
type electrumHistoryItem struct {
	Height int64  `json:"height"`
	TxHash string `json:"tx_hash"`
}

// GetHistory returns the confirmed+mempool history of a script (by scripthash).
func (c *electrumClient) GetHistory(ctx context.Context, scriptHash string) ([]electrumHistoryItem, error) {
	var out []electrumHistoryItem
	err := c.call(ctx, "blockchain.scripthash.get_history", []any{scriptHash}, &out)
	return out, err
}

// electrumTokenData mirrors the token_data object Fulcrum attaches to UTXOs
// carrying CashTokens (protocol >= 1.5.0).
type electrumTokenData struct {
	Category string `json:"category"` // hex, display (big-endian) order
	Amount   string `json:"amount"`   // fungible amount as string
	NFT      *struct {
		Capability string `json:"capability"` // "none" | "mutable" | "minting"
		Commitment string `json:"commitment"` // hex
	} `json:"nft"`
}

// electrumUTXO is one entry of blockchain.scripthash.listunspent.
type electrumUTXO struct {
	Height    int64              `json:"height"`
	TxPos     uint32             `json:"tx_pos"`
	TxHash    string             `json:"tx_hash"`
	Value     int64              `json:"value"` // satoshis
	TokenData *electrumTokenData `json:"token_data"`
}

// ListUnspent returns the UTXOs locked to a script (by scripthash), including
// token data on servers that support it.
func (c *electrumClient) ListUnspent(ctx context.Context, scriptHash string) ([]electrumUTXO, error) {
	var out []electrumUTXO
	err := c.call(ctx, "blockchain.scripthash.listunspent", []any{scriptHash}, &out)
	return out, err
}

// GetRawTransaction fetches a transaction's raw hex by txid. We parse raw
// transactions locally (bchtx.go) instead of relying on server-specific
// verbose formats.
func (c *electrumClient) GetRawTransaction(ctx context.Context, txid string) ([]byte, error) {
	var rawHex string
	if err := c.call(ctx, "blockchain.transaction.get", []any{txid, false}, &rawHex); err != nil {
		return nil, err
	}
	return hex.DecodeString(rawHex)
}

// Broadcast submits a raw transaction and returns its txid.
func (c *electrumClient) Broadcast(ctx context.Context, rawTx []byte) (string, error) {
	var txid string
	err := c.call(ctx, "blockchain.transaction.broadcast", []any{hex.EncodeToString(rawTx)}, &txid)
	return txid, err
}

// RelayFee returns the server's minimum relay fee in BCH/kB.
func (c *electrumClient) RelayFee(ctx context.Context) (float64, error) {
	var fee float64
	err := c.call(ctx, "blockchain.relayfee", nil, &fee)
	return fee, err
}

// BlockHeight returns the current chain tip height, used to compute how many
// confirmations a claim has. It subscribes to headers (the standard way to get
// the tip) and reads the returned tip height.
func (c *electrumClient) BlockHeight(ctx context.Context) (int64, error) {
	var tip struct {
		Height int64 `json:"height"`
	}
	if err := c.call(ctx, "blockchain.headers.subscribe", nil, &tip); err != nil {
		return 0, err
	}
	return tip.Height, nil
}

// scriptHash computes the Electrum protocol identifier for a locking script:
// hex of sha256(script) with the byte order reversed.
func scriptHash(script []byte) string {
	sum := sha256.Sum256(script)
	// Reverse into display order.
	for i, j := 0, len(sum)-1; i < j; i, j = i+1, j-1 {
		sum[i], sum[j] = sum[j], sum[i]
	}
	return hex.EncodeToString(sum[:])
}
