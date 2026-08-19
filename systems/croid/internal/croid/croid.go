// Package croid generates and validates Crossref Research Object IDs.
//
// A CROID is a URL-safe identifier of exactly 32 characters drawn from the
// alphabet [0-9][A-Z][a-z][-_]. The characters are produced from 192 bits of
// cryptographic randomness (6 bits per character), so the result is uniformly
// distributed and carries no modulus bias.
package croid

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// Length is the number of characters in every CROID.
const Length = 32

// Alphabet is the set of characters a CROID may contain. It has exactly 64
// (2^6) members so each character maps to a clean 6-bit chunk.
const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-_"

const (
	bitsPerChar = 6
	bytesNeeded = Length * bitsPerChar / 8 // 192 bits == 24 bytes
)

// Generate returns a new random 32-character CROID.
func Generate() (string, error) {
	buf := make([]byte, bytesNeeded)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("croid: generate random bytes: %w", err)
	}

	var sb strings.Builder
	sb.Grow(Length)
	for i := 0; i < Length; i++ {
		idx := 0
		for b := 0; b < bitsPerChar; b++ {
			idx = idx<<1 | bitAt(buf, i*bitsPerChar+b)
		}
		sb.WriteByte(Alphabet[idx])
	}
	return sb.String(), nil
}

// bitAt returns the i-th bit of buf, treating buf as a big-endian bit stream
// where bit 0 is the most significant bit of buf[0].
func bitAt(buf []byte, i int) int {
	return int(buf[i/8] >> uint(7-i%8) & 1)
}

// Valid reports whether s is a well-formed CROID: exactly Length characters,
// every one of which is present in Alphabet.
func Valid(s string) bool {
	if len(s) != Length {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(Alphabet, rune(s[i])) {
			return false
		}
	}
	return true
}
