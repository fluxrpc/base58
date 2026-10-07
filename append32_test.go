package base58

import (
	"bytes"
	"fmt"
	"testing"
)

func TestAppendEncode32_CapacityAndLeadingZeros(t *testing.T) {
	testAppendEncode32CapacityAndLeadingZeros(t)
}

func testAppendEncode32CapacityAndLeadingZeros(t *testing.T) {
	for zeros := 0; zeros <= 32; zeros++ {
		var src [32]byte
		for i := zeros; i < len(src); i++ {
			src[i] = 0xff
		}
		encoded := encodeDivisionReference(src[:])
		for _, prefix := range []string{"", "pubkey="} {
			for _, spare := range []int{0, 31, 32, len(encoded) - 1, len(encoded), EncodedMaxLen32, EncodedMaxLen32 + 16} {
				t.Run(fmt.Sprintf("zeros=%d/prefix=%d/spare=%d", zeros, len(prefix), spare), func(t *testing.T) {
					backing := bytes.Repeat([]byte{0xa5}, len(prefix)+spare+16)
					copy(backing, prefix)
					dst := backing[: len(prefix) : len(prefix)+spare]
					got := AppendEncode32(dst, &src)
					if string(got) != prefix+encoded {
						t.Fatalf("got %q, want %q", got, prefix+encoded)
					}
					untouched := len(prefix)
					if spare >= len(encoded) {
						if &got[0] != &backing[0] {
							t.Fatal("allocated despite sufficient capacity")
						}
						untouched = len(got)
					}
					for i, b := range backing[untouched:] {
						if b != 0xa5 {
							t.Fatalf("unexpected write at backing[%d]", untouched+i)
						}
					}
				})
			}
		}
		if got := AppendEncode32(nil, &src); string(got) != encoded {
			t.Fatalf("nil destination: got %q, want %q", got, encoded)
		}
	}
}
