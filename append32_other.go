//go:build !amd64

package base58

func encode32Fast(src *[32]byte) ([]byte, bool) {
	return nil, false
}

// AppendEncode32 appends the base58 encoding of src to dst and returns the
// extended buffer. It allocates only if dst has insufficient capacity.
func AppendEncode32(dst []byte, src *[32]byte) []byte {
	return appendEncode32Generic(dst, src)
}
