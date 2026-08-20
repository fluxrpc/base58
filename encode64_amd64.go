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
	if src[0] == 0 {
		return encode64LeadingAVX2(src)
	}
	// The 96-byte size class costs the same allocation as 88 bytes and lets
	// this owned-output path use three non-overlapping vector stores. Pointing
	// the string past the raw form's leading digits avoids realigning them.
	out := new([96]byte)
	skip := encode64FullOwnedAVX2(src, &out[0])
	p := (*byte)(unsafe.Add(unsafe.Pointer(out), skip))
	return unsafe.String(p, raw58Sz64-skip)
}

//go:noinline
func encode64LeadingAVX2(src *[64]byte) string {
	out := new([96]byte)
	n := encode64FullAVX2(src, &out[0], true)
	return unsafe.String(&out[0], n)
}
