package stringid

// code and credits to github.com/moby/moby/pkg/stringid

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

// ShortLen is the Job id len
const ShortLen = 32

// credits: github.com/moby/moby/pkg/stringid
func GenerateID() string {
	b := make([]byte, 32)
	r := rand.Reader
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			panic(err) // This shouldn't happen
		}
		id := hex.EncodeToString(b)
		// if we try to parse the truncated for as an int and we don't have
		// an error then the value is all numeric and causes issues when
		// used as a hostname. ref #3869
		return truncateID(id)
	}
}

// credits: github.com/moby/moby/pkg/stringid
func truncateID(id string) string {
	trimTo := ShortLen
	if len(id) < ShortLen {
		trimTo = len(id)
	}
	return id[:trimTo]
}
