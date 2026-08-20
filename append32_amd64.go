//go:build amd64

package base58

// AppendEncode32 appends the base58 encoding of src to dst.  Its AVX2
// no-grow path is implemented end-to-end in assembly; allocation and scalar
// fallback are handled by appendEncode32Slow.
func AppendEncode32(dst []byte, src *[32]byte) []byte

func encode32Fast(src *[32]byte) ([]byte, bool) {
	if !useAVX2 {
		return nil, false
	}
	storage := new([EncodedMaxLen32]byte)
	return AppendEncode32(storage[:0], src), true
}

func appendEncode32Slow(dst []byte, src *[32]byte) []byte {
	if !useAVX2 {
		return appendEncode32Generic(dst, src)
	}
	start := len(dst)
	grown := make([]byte, start, start+EncodedMaxLen32)
	copy(grown, dst)
	return AppendEncode32(grown, src)
}
