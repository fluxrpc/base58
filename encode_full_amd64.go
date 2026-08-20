//go:build amd64

// Derived from Apache-2.0-licensed Firedancer/solana-go work and modified by
// AlphaBatem Labs. See LICENSE, LICENSE-MIT, and NOTICE.
package base58

//go:noescape
func encode64FullAVX2(src *[64]byte, dst *byte, clobberTail bool) int

var encFullTable64 [binarySz64][20]uint64

func init() {
	if !useAVX2 {
		return
	}
	for i, row := range encTable64 {
		for k, v := range row {
			encFullTable64[i][k] = uint64(v)
		}
	}
}

// Constants referenced by encode_full_amd64.s.
//
//nolint:unused
var (
	encFullBswap  = fusedRep(3, 2, 1, 0, 7, 6, 5, 4, 11, 10, 9, 8, 15, 14, 13, 12)
	encFullSpread = fusedRep(0, 1, 1, 1, 1, 8, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)
	encFullB8     = fusedRep(8)
	encFullB16    = fusedRep(16)
	encFullB21    = fusedRep(21)
	encFullB32    = fusedRep(32)
	encFullB43    = fusedRep(43)
	encFullBm6    = fusedRep(0xFA)
	encFullBm7    = fusedRep(0xF9)
	encFullBm49   = fusedRep(0xCF)
	encFullD58    = fusedRep(58, 0, 0, 0)
	encFullDivA   = fusedRep(0x09, 0xCB, 0x3D, 0x8D, 0, 0, 0, 0)
	encFullDivB   = fusedRep(0x93, 0x20, 0xED, 0x4D, 0, 0, 0, 0)
)
