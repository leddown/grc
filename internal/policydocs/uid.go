package policydocs

import (
	"crypto/rand"
	"fmt"
)

// NewUID returns a random RFC 4122 version 4 UUID, the identity a section
// keeps inside a Studio document.
func NewUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("policydocs: crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
