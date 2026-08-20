//go:build amd64

// Block-radix performance work informed by base58-turbo.
// Copyright 2026 AlphaBatem Labs.
// SPDX-License-Identifier: MIT OR Apache-2.0

package base58

import "unsafe"

var blockP512Wide [20]uint64

func init() {
	for i := range pow2_512_base58_5 {
		blockP512Wide[i] = uint64(pow2_512_base58_5[len(pow2_512_base58_5)-1-i])
	}
}

//go:noescape
func encodeConvolveP512AVX2(padded *uint32, columns int, tmp *uint64)

func encodeConvolveP512All(digits []uint32, count, columns int, tmp []uint64) bool {
	if !useAVX2 {
		return false
	}
	clear(digits[count:columns])
	padded := (*uint32)(unsafe.Add(unsafe.Pointer(&digits[0]), -17*4))
	encodeConvolveP512AVX2(padded, columns, &tmp[0])
	return true
}

func encodeConvolveP512(digits []uint32, count int, tmp []uint64) {
	for column := 17; column < count; column++ {
		tmp[column] = encodeMulP512(digits[column-17 : column+1])
	}
}

func encodePaddedFixedTail(src, out []byte) (int, bool) {
	if !useAVX2 {
		return 0, false
	}
	var padded [64]byte
	copy(padded[64-len(src):], src)
	var scratch [EncodedMaxLen64]byte
	encoded, ok := appendEncode64Fast(scratch[:0], &padded)
	if !ok {
		return 0, false
	}
	encoded = encoded[64-len(src):]
	p := len(out) - len(encoded)
	copy(out[p:], encoded)
	return p, true
}
