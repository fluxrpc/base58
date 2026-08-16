//go:build !amd64

package base58

// Encode64 encodes a 64-byte array to a base58 string.
func Encode64(src *[64]byte) string {
	return encode64Generic(src)
}
