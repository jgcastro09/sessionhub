package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a collision-resistant, sortable-enough identifier without
// introducing an identity dependency. The timestamp lives in durable records;
// identifiers intentionally reveal no machine or user information.
func New(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("generate %s id: %w", prefix, err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
