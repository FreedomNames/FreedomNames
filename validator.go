package main

import (
	"errors"
	"fmt"
	"unicode/utf8"

	record "github.com/libp2p/go-libp2p-record"
)

// FreedomNameValidator is a Validator that validates freedom names namespace.
type FreedomNameValidator struct{}

// Validate validates a freedom name (FN) record.
func (pkv FreedomNameValidator) Validate(key string, value []byte) error {
	ns, key, err := record.SplitKey(key)
	if err != nil {
		return err
	}
	if ns != "fn" {
		return errors.New("namespace not 'fn'")
	}

	// Check if the key is a valid string
	if !utf8.ValidString(key) {
		return fmt.Errorf("key is not valid UTF-8")
	}
	// keyhash := []byte(key)
	// if _, err := mh.Cast(keyhash); err != nil {
	// 	return fmt.Errorf("key did not contain valid multihash: %s", err)
	// }
	return nil
}

// Select conforms to the Validator interface. Select the first value (index 0)
//
// TODO: Maybe the newest or most appropriate value should be selected?
func (pkv FreedomNameValidator) Select(k string, vals [][]byte) (int, error) {
	return 0, nil
}
