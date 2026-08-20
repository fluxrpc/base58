//go:build amd64

// Derived from Apache-2.0-licensed Firedancer/solana-go work and modified by
// AlphaBatem Labs. See LICENSE, LICENSE-MIT, and NOTICE.
package base58

//go:noescape
func decode32FusedAVX2(s *byte, n int, dst *[32]byte) int

//go:noescape
func decode32Fused43AVX2(s *byte, dst *[32]byte) int

//go:noescape
func decode64FusedAVX2(s *byte, n int, dst *[64]byte) int

//go:noescape
func decode64Fused87AVX2(s *byte, dst *[64]byte) int

// Full decoder tables, widened to qwords for VPMULUDQ. The existing AVX2
// matrix path only needs the non-zero head rows; a fused decoder needs every
// row because it never materializes a Go intermediate.
var (
	decFusedTable32 [intermediateSz32][binarySz32]uint64
	decFusedTable64 [intermediateSz64][binarySz64]uint64
)

func init() {
	if !useAVX2 {
		return
	}
	for i, row := range decTable32 {
		for k, v := range row {
			decFusedTable32[i][k] = uint64(v)
		}
	}
	for i, row := range decTable64 {
		for k, v := range row {
			decFusedTable64[i][k] = uint64(v)
		}
	}
}

func fusedRep(b ...byte) (out [32]byte) {
	for i := range out {
		out[i] = b[i%len(b)]
	}
	return
}

// Constants referenced by decode_fused_amd64.s.
//
//nolint:unused
var (
	decFusedIota = fusedRep(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	// For the common 43-character Decode32 case, only the low lane needs
	// two zero padding bytes; the high lane is already group-aligned.
	decFusedPad43 = [32]byte{
		0x80, 0x80, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13,
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	}
	decFusedPad87 = [32]byte{
		0x80, 0x80, 0x80, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	}
	decFusedP    = fusedRep(0, 1, 2, 3, 5, 6, 7, 8, 10, 11, 12, 13, 0x80, 0x80, 0x80, 0x80)
	decFusedQ    = fusedRep(4, 0x80, 0x80, 0x80, 9, 0x80, 0x80, 0x80, 14, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80)
	decFusedTail = [32]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0x80}
	decFusedW1    = fusedRep(58, 1)
	decFusedW2    = fusedRep(0x24, 0x0D, 1, 0) // int16 {3364, 1}
	decFusedLoNib = fusedRep(20, 31, 31, 31, 31, 31, 31, 31, 31, 29, 30, 10, 2, 10, 10, 8)
	decFusedHiNib = fusedRep(0, 0, 0, 1, 2, 4, 8, 16, 0, 0, 0, 0, 0, 0, 0, 0)
	// Validation produces class bits 1, 2, 4, 8, and 16 for the five
	// populated ASCII nibbles. PSHUFB wraps class 16 to slot zero.
	decFusedBase = fusedRep(65, 49, 56, 0, 58, 0, 0, 0, 64, 0, 0, 0, 0, 0, 0, 0)
	// The J-N and m-o ranges sit one byte beyond their class's base.
	decFusedCorr = fusedRep(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 10, 10, 10)
	decFusedB1   = fusedRep(1)
	decFusedB15  = fusedRep(0x0F)
	decFusedD58  = fusedRep(58, 0, 0, 0)
)
