//go:build amd64

package base58

import "unsafe"

// Decode32 decodes a base58 string into a 32-byte array. AVX2 inputs in the
// fixed-width range complete validation and conversion in one kernel call.
func Decode32(encoded string, dst *[32]byte) error {
	n := len(encoded)
	if n == 0 || n > raw58Sz32 {
		return ErrInvalidLength
	}
	if useAVX2 && n >= 32 {
		var status int
		if n == 43 {
			status = decode32Fused43AVX2(unsafe.StringData(encoded), dst)
		} else {
			status = decode32FusedAVX2(unsafe.StringData(encoded), n, dst)
		}
		if status == 0 {
			return nil
		}
		switch status {
		case 1:
			return ErrInvalidChar
		case 2:
			return ErrValueTooLarge
		default:
			return validateLeadingZeros(encoded, dst[:])
		}
	}
	return decode32Generic(encoded, dst)
}

// Decode64 decodes a base58 string into a 64-byte array. AVX2 inputs in the
// fixed-width range complete validation and conversion in one kernel call.
func Decode64(encoded string, dst *[64]byte) error {
	n := len(encoded)
	if n == 0 || n > raw58Sz64 {
		return ErrInvalidLength
	}
	if useAVX2 && n >= 64 {
		var status int
		if n == 87 {
			status = decode64Fused87AVX2(unsafe.StringData(encoded), dst)
		} else {
			status = decode64FusedAVX2(unsafe.StringData(encoded), n, dst)
		}
		if status == 0 {
			return nil
		}
		switch status {
		case 1:
			return ErrInvalidChar
		case 2:
			return ErrValueTooLarge
		default:
			return validateLeadingZeros(encoded, dst[:])
		}
	}
	return decode64Generic(encoded, dst)
}
