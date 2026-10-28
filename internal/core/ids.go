// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns a lexicographically sortable, 26-character ULID: 48 bits of millisecond
// timestamp followed by 80 bits of randomness. It is safe for concurrent use.
func NewID() string {
	var raw [16]byte

	// The timestamp occupies the first 48 bits; the last two bytes of the big-endian
	// representation are always zero for millisecond precision.
	var timestamp [8]byte
	binary.BigEndian.PutUint64(timestamp[:], uint64(time.Now().UnixMilli()))
	copy(raw[:6], timestamp[2:])

	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(raw[6:])

	value := new(big.Int).SetBytes(raw[:])
	base := big.NewInt(32)
	remainder := new(big.Int)
	out := make([]byte, 26)
	for i := len(out) - 1; i >= 0; i-- {
		value.DivMod(value, base, remainder)
		out[i] = crockford[remainder.Int64()]
	}
	return string(out)
}
