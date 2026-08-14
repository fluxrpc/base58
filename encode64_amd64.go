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
	out := make([]byte, EncodedMaxLen64)
	n := encode64DirectAVX2(src, &out[0])
	return unsafe.String(unsafe.SliceData(out), n)
}
