package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// cliPut is the common author action LibreWeb's editor triggers: upload a file's
// content to a running node, then point a name at it. In one step it:
//
//  1. POSTs the file bytes to the node's /content (content-addressed store),
//
//  2. stages a single CONTENT record with the returned hash,
//
//  3. signs and publishes the name.
//
//     freedom put <label> <file> [--api URL] [--ttl SECONDS]
func cliPut(args []string) error {
	positional, flags := popPositionals(args, 2)
	if len(positional) != 2 {
		return fmt.Errorf("usage: freedom put <label> <file> [--api URL] [--ttl SECONDS]")
	}
	label, file := positional[0], positional[1]
	api := flagValue(flags, "--api", defaultAPI)
	ttl := uint32(300)
	if v := flagValue(flags, "--ttl", ""); v != "" {
		var parsed uint64
		if _, err := fmt.Sscanf(v, "%d", &parsed); err != nil {
			return fmt.Errorf("invalid --ttl %q: %w", v, err)
		}
		ttl = uint32(parsed)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}

	hash, err := uploadContent(api, data)
	if err != nil {
		return err
	}
	fmt.Printf("Uploaded %s (%d bytes) -> %s\n", file, len(data), hash)

	// A name published this way points solely at its content.
	records := []RR{{Type: RecordTypeCONTENT, Value: hash, TTL: ttl}}
	if err := saveStaged(label, records); err != nil {
		return err
	}
	return publishRecords(api, label, records)
}

// uploadContent POSTs raw bytes to a node's /content endpoint and returns the
// content hash the node assigned.
func uploadContent(api string, data []byte) (string, error) {
	resp, err := http.Post(api+"/content", "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("upload to %s: %w", api, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("node rejected content (%d): %s", resp.StatusCode, string(body))
	}
	var out struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Hash == "" {
		return "", fmt.Errorf("unexpected /content response: %s", string(body))
	}
	return out.Hash, nil
}
