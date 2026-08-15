package bloom

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

// TestPositionsFirstElement verifies that the first position returned by
// positions() equals h1 % m, where h1 is the first 8 bytes of SHA-256
// interpreted as big-endian uint64. The Kirsch-Mitzenmacher formula is:
// pos[i] = (h1 + i*h2) % m, so pos[0] must be h1 % m.
func TestPositionsFirstElement(t *testing.T) {
	f, err := New(1000, 0.01)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	item := "test-element"
	sum := sha256.Sum256([]byte(item))
	h1 := binary.BigEndian.Uint64(sum[0:8])
	expectedFirst := h1 % f.m

	positions := f.positions(item)
	if len(positions) == 0 {
		t.Fatal("positions returned empty slice")
	}
	if positions[0] != expectedFirst {
		t.Fatalf("positions[0] = %d, expected h1%%m = %d (h1=%d, m=%d)",
			positions[0], expectedFirst, h1, f.m)
	}
}
