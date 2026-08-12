//go:build !amd64

package base58

import "encoding/binary"

// encodeMatMul64 computes the full 64-byte encode matmul, including the
// mid-way mini-reduction that prevents u64 overflow (matches Firedancer).
// Row bounds skip the tables' zero entries (~2x less work).
func encodeMatMul64(src *[64]byte, intermediate *[intermediateSz64]uint64) {
	var bin [binarySz64]uint32
	for i := range binarySz64 {
		bin[i] = binary.BigEndian.Uint32(src[i*4 : i*4+4])
	}

	// First 8 limbs.
	for i := 0; i < 8; i++ {
		b := uint64(bin[i])
		row := &encTable64[i]
		for k, hi := int(encRowBounds64[i][0]), int(encRowBounds64[i][1]); k < hi; k++ {
			intermediate[k+1] += b * uint64(row[k])
		}
	}

	// Mini-reduction to prevent overflow before the second half.
	intermediate[intermediateSz64-3] += intermediate[intermediateSz64-2] / r1div
	intermediate[intermediateSz64-2] %= r1div

	// Last 8 limbs.
	for i := 8; i < binarySz64; i++ {
		b := uint64(bin[i])
		row := &encTable64[i]
		for k, hi := int(encRowBounds64[i][0]), int(encRowBounds64[i][1]); k < hi; k++ {
			intermediate[k+1] += b * uint64(row[k])
		}
	}
}

// decodeMatMul64 computes bin[k] = sum_i intermediate[i] * decTable64[i][k].
// Each product is ≤ 2^62 and the per-column sum of 18 terms stays under 2^64
// (verified by Firedancer analysis). Row bounds skip the table's zero entries.
func decodeMatMul64(intermediate *[intermediateSz64]uint64, bin *[binarySz64]uint64) {
	for i := range intermediateSz64 {
		v := intermediate[i]
		row := &decTable64[i]
		for k, hi := int(decRowBounds64[i][0]), int(decRowBounds64[i][1]); k < hi; k++ {
			bin[k] += v * uint64(row[k])
		}
	}
}
