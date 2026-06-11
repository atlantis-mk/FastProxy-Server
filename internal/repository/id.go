package repository

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID(prefix string) string {
	var bytes [6]byte
	_, _ = rand.Read(bytes[:])
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
