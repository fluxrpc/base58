//go:build amd64

package base58

import "unsafe"

// Encode64 encodes a 64-byte array to a base58 string.
//
// Allocates exactly once. On AVX2, the complete conversion after allocation
// stays in the fused assembly kernel.
func Encode64(src *[64]byte) string {
	if !useAVX2 {
		return encode64Generic(src)
	}
	// The 96-byte size class costs the same allocation as 88 bytes and lets
	// this owned-output path use three non-overlapping vector stores. The
	// returned string is still trimmed to the exact encoded length.
	out := make([]byte, 96)
	n := encode64FullAVX2(src, &out[0], true)
	return unsafe.String(unsafe.SliceData(out), n)
}
