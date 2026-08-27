//go:build amd64

package base58

// AppendEncode32 appends the base58 encoding of src to dst and returns the
// extended buffer. It allocates only if dst has insufficient capacity.
func AppendEncode32(dst []byte, src *[32]byte) []byte {
	if !useAVX2 {
		return appendEncode32Generic(dst, src)
	}

	start := len(dst)
	if cap(dst)-start < EncodedMaxLen32 {
		grown := make([]byte, start, start+EncodedMaxLen32)
		copy(grown, dst)
		dst = grown
	}
	out := dst[:start+EncodedMaxLen32]
	n := appendEncode32AVX2(&out[start], src)
	return out[:start+n]
}

//go:noescape
func appendEncode32AVX2(dst *byte, src *[32]byte) int

func encode32Fast(src *[32]byte) ([]byte, bool) {
	if !useAVX2 {
		return nil, false
	}
	storage := new([EncodedMaxLen32]byte)
	return AppendEncode32(storage[:0], src), true
}
