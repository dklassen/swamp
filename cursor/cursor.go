// Package cursor makes the opaque pagination cursors search_postings
// hands the agent (#219, RFC 0006). A cursor records where the previous
// page ended, in which sort order, for which filters. It's versioned,
// base64url-encoded JSON: opaque, so its shape can change without callers
// noticing, but not secret -- anyone can decode it, so Decode treats it
// as untrusted input. Keyset cursors don't expire: nothing is kept on the
// server.
package cursor

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// version is the token format this package writes and reads. A token of
// any other version is invalid; bump it when Key changes shape.
const version = 1

var (
	// ErrInvalid is a token this package didn't make, or can't read.
	ErrInvalid = errors.New("cursor: invalid")
	// ErrMismatch is a valid token from a different search: another sort
	// order, or other filters. Its position means nothing here.
	ErrMismatch = errors.New("cursor: belongs to a different search")
)

// Key is where a page ended: the sort order, the filters' Fingerprint,
// and the last row's sort key.
type Key struct {
	Sort    string
	Filters string
	// ID is the last row's posting ID: the whole key for ID orders, the
	// tie-breaker for any other.
	ID int64
}

// token is Key as encoded, with its format version.
type token struct {
	V       int    `json:"v"`
	Sort    string `json:"sort"`
	Filters string `json:"filters"`
	ID      int64  `json:"id"`
}

// Encode makes the opaque token for key.
func Encode(key Key) string {
	encoded, err := json.Marshal(token{V: version, Sort: key.Sort, Filters: key.Filters, ID: key.ID})
	if err != nil {
		// A struct of strings and ints always encodes.
		panic(fmt.Sprintf("cursor: encode %+v: %v", key, err))
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// Decode reads a token Encode made, for a search in sort order sort with
// filters' Fingerprint filters. It returns ErrInvalid for anything that
// isn't such a token, and ErrMismatch for one made for another search.
func Decode(t, sort, filters string) (Key, error) {
	raw, err := base64.RawURLEncoding.DecodeString(t)
	if err != nil {
		return Key{}, fmt.Errorf("%w: not base64url", ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var tok token
	if err := decoder.Decode(&tok); err != nil {
		return Key{}, fmt.Errorf("%w: not a cursor", ErrInvalid)
	}
	switch {
	case tok.V != version:
		return Key{}, fmt.Errorf("%w: version %d, want %d", ErrInvalid, tok.V, version)
	case tok.ID <= 0:
		return Key{}, fmt.Errorf("%w: no position", ErrInvalid)
	case tok.Sort != sort || tok.Filters != filters:
		return Key{}, ErrMismatch
	}
	return Key{Sort: tok.Sort, Filters: tok.Filters, ID: tok.ID}, nil
}

// Fingerprint is a short hash of a search's filters, so a cursor can tell
// it's being reused with different ones. filters is JSON-encoded first:
// map keys sort, so equal filters give equal fingerprints. It must be
// encodable (plain values), as a search's filters are.
func Fingerprint(filters any) string {
	encoded, err := json.Marshal(filters)
	if err != nil {
		panic(fmt.Sprintf("cursor: fingerprint %#v: %v", filters, err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:8])
}
