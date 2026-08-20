//go:build !amd64

package base58

// Decode32 decodes a base58 string into a 32-byte array.
func Decode32(encoded string, dst *[32]byte) error {
	if len(encoded) == 0 || len(encoded) > raw58Sz32 {
		return ErrInvalidLength
	}
	return decode32Generic(encoded, dst)
}

// Decode64 decodes a base58 string into a 64-byte array.
func Decode64(encoded string, dst *[64]byte) error {
	if len(encoded) == 0 || len(encoded) > raw58Sz64 {
		return ErrInvalidLength
	}
	return decode64Generic(encoded, dst)
}
