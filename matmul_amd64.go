//go:build amd64

// Derived from Apache-2.0-licensed Firedancer/solana-go work and modified by
// AlphaBatem Labs. See LICENSE, LICENSE-MIT, and NOTICE.
package base58

//go:noescape
func encodeMatMul32Scalar(src *[32]byte, intermediate *[intermediateSz32]uint64)

//go:noescape
func decodeMatMul32Scalar(intermediate *[intermediateSz32]uint64, bin *[binarySz32]uint64)

//go:noescape
func encodeMatMul32AVX2(src *[32]byte, intermediate *[intermediateSz32]uint64)

//go:noescape
func decodeMatMul32AVX2(intermediate *[intermediateSz32]uint64, bin *[binarySz32]uint64)

// encodeMatMul32 dispatches to the AVX2 kernel when available.
func encodeMatMul32(src *[32]byte, intermediate *[intermediateSz32]uint64) {
	if useAVX2 {
		encodeMatMul32AVX2(src, intermediate)
		return
	}
	encodeMatMul32Scalar(src, intermediate)
}

// decodeMatMul32 dispatches to the AVX2 kernel when available.
func decodeMatMul32(intermediate *[intermediateSz32]uint64, bin *[binarySz32]uint64) {
	if useAVX2 {
		decodeMatMul32AVX2(intermediate, bin)
		return
	}
	decodeMatMul32Scalar(intermediate, bin)
}

//go:noescape
func encodeMatMul64Head(src *[64]byte, intermediate *[intermediateSz64]uint64)

//go:noescape
func encodeMatMul64HeadAVX2(src *[64]byte, intermediate *[intermediateSz64]uint64)

//go:noescape
func decodeMatMul64AVX2(intermediate *[intermediateSz64]uint64, bin *[binarySz64]uint64)

//go:noescape
func decode32Write(intermediate *[intermediateSz32]uint64, dst *[32]byte) bool

//go:noescape
func decode64Write(intermediate *[intermediateSz64]uint64, dst *[64]byte) bool

// encWide64/decWide64 are the head rows of encTable64/decTable64 with
// entries zero-extended to u64 lanes for VPMULUDQ (which multiplies the low
// 32 bits of each 64-bit lane). encWide64 is padded to 20 columns so the
// 5th YMM accumulator has a full block to read.
var (
	encWide64 [8][20]uint64
	decWide64 [9][16]uint64
	encWide32 [8][8]uint64
	decWide32 [9][8]uint64
)

func init() {
	if !useAVX2 {
		return
	}
	for i := range encWide64 {
		for k := range intermediateSz64 - 1 {
			encWide64[i][k] = uint64(encTable64[i][k])
		}
	}
	for i := range decWide64 {
		for k := range binarySz64 {
			decWide64[i][k] = uint64(decTable64[i][k])
		}
	}
	for i := range encWide32 {
		for k := range intermediateSz32 - 1 {
			encWide32[i][k] = uint64(encTable32[i][k])
		}
	}
	for i := range decWide32 {
		for k := range binarySz32 {
			decWide32[i][k] = uint64(decTable32[i][k])
		}
	}
}

//go:noescape
func decodeMatMul64Head(intermediate *[intermediateSz64]uint64, bin *[binarySz64]uint64)

// decodeMatMul64 computes bin[k] = sum_i intermediate[i] * decTable64[i][k].
// Rows 0-8 run in decodeMatMul64Head assembly; rows 9-17 of decTable64 are
// decTable32's rows shifted 8 columns, so the existing decodeMatMul32
// assembly computes their sums, merged with 8 adds. Each per-column total
// fits u64 (Firedancer analysis), so the split accumulation is exact.
func decodeMatMul64(intermediate *[intermediateSz64]uint64, bin *[binarySz64]uint64) {
	if useAVX2 {
		decodeMatMul64AVX2(intermediate, bin)
		return
	}
	decodeMatMul64Head(intermediate, bin)

	var tail [binarySz32]uint64
	decodeMatMul32((*[intermediateSz32]uint64)(intermediate[9:]), &tail)
	for k := range binarySz32 {
		bin[k+8] += tail[k]
	}
}

// encodeMatMul64 computes the full 64-byte encode matmul, including the
// mid-way mini-reduction that prevents u64 overflow (matches Firedancer).
//
// Rows 0-7 run in encodeMatMul64Head assembly. Rows 8-15 of encTable64 are
// encTable32's rows shifted 9 columns — they describe the low 32 bytes —
// so the existing encodeMatMul32 assembly computes their sums, merged in
// with 8 adds. Add order differs from the reference row-by-row loop but the
// mathematical sum is identical and fits u64 after the mini-reduction.
func encodeMatMul64(src *[64]byte, intermediate *[intermediateSz64]uint64) {
	if useAVX2 {
		encodeMatMul64HeadAVX2(src, intermediate)
	} else {
		encodeMatMul64Head(src, intermediate)
	}

	// Mini-reduction before folding in the low half.
	intermediate[intermediateSz64-3] += intermediate[intermediateSz64-2] / r1div
	intermediate[intermediateSz64-2] %= r1div

	var tail [intermediateSz32]uint64
	encodeMatMul32((*[32]byte)(src[32:]), &tail)
	for j := 1; j < intermediateSz32; j++ {
		intermediate[j+9] += tail[j]
	}
}
