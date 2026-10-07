//go:build amd64

package base58

// AppendEncode32 appends the base58 encoding of src to dst and returns the
// extended buffer. It allocates only if dst has insufficient capacity.
func AppendEncode32(dst []byte, src *[32]byte) []byte {
	if !useAVX2 {
		return appendEncode32Generic(dst, src)
	}
	if cap(dst)-len(dst) < EncodedMaxLen32 {
		return appendEncode32GenericCapacity(dst, src, EncodedMaxLen32)
	}
	start := len(dst)
	dst = dst[:start+EncodedMaxLen32]
	n := encode32AppendAVX2(src, &dst[start])
	return dst[:start+n]
}

// encode32AppendAVX2 writes only the encoded bytes to dst, which must have
// room for EncodedMaxLen32 bytes, and returns their count. Neither pointer
// is retained; the Go wrapper preserves dst's relationship to its result.
//
//go:noescape
func encode32AppendAVX2(src *[32]byte, dst *byte) int

func encode32Fast(src *[32]byte) ([]byte, bool) {
	if !useAVX2 {
		return nil, false
	}
	storage := new([EncodedMaxLen32]byte)
	return AppendEncode32(storage[:0], src), true
}
