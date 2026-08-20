//go:build !amd64

// Block-radix performance work informed by base58-turbo.
// Copyright 2026 AlphaBatem Labs.
// SPDX-License-Identifier: MIT OR Apache-2.0

package base58

func encodeConvolveP512All(_ []uint32, _, _ int, _ []uint64) bool {
	return false
}

func encodePaddedFixedTail(_ []byte, _ []byte) (int, bool) {
	return 0, false
}

func encodeConvolveP512(digits []uint32, count int, tmp []uint64) {
	for column := 17; column < count; column++ {
		tmp[column] = encodeMulP512(digits[column-17 : column+1])
	}
}
