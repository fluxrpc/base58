//go:build amd64

// Derived from Apache-2.0-licensed Firedancer work and modified by AlphaBatem
// Labs. See LICENSE, LICENSE-MIT, and NOTICE.
package base58

//go:noescape
func encode64DirectAVX2(src *[64]byte, out *byte) int

func appendEncode64Fast(dst []byte, src *[64]byte) ([]byte, bool) {
	start := len(dst)
	if !useAVX2 || cap(dst)-start < EncodedMaxLen64 {
		return nil, false
	}
	storage := dst[:cap(dst)]
	n := encode64FullAVX2(src, &storage[start], false)
	return storage[:start+n], true
}

//go:noescape
func digitsToChars8(v *uint64, out *byte)

//go:noescape
func digitsToChars16Fast(v *uint64, out *byte)

// digitsToChars16FastRegs is an assembly-internal register-ABI entry point.
// It is declared solely so assembly vet can validate the symbol.
//
//go:noescape
func digitsToChars16FastRegs()

//go:noescape
func digitsToChars16(v *uint64, out *byte)

// extractChars32 converts the 9 base-58^5 values to 45 chars. Measured: the
// scalar pair-LUT path beats the digitsToChars8 kernel at this size (48.4 vs
// 51.2 ns AppendEncode32) — the kernel's constant setup dominates for only
// 9 elements. SIMD extraction is used for the 64-byte and batch paths.
func extractChars32(intermediate *[intermediateSz32]uint64, raw *[raw58Buf32]byte) {
	extractChars32Generic(intermediate, raw)
}

// extractChars64 converts the 18 base-58^5 values to 90 chars. Two SIMD
// kernel calls cover elements 0-15; 16-17 run scalar (also overwriting the
// second call's overhang at raw[80:86]).
func extractChars64(intermediate *[intermediateSz64]uint64, raw *[raw58Sz64]byte) {
	if useAVX2 {
		digitsToChars16Fast(&intermediate[0], &raw[0])
		extractChars5(uint32(intermediate[16]), raw[80:85])
		extractChars5(uint32(intermediate[17]), raw[85:90])
		return
	}
	extractChars64Generic(intermediate, raw)
}

//go:noescape
func digitsToChars36(v *uint64, out *byte)

// extractCharsFlat36 converts four keys' 36 flat intermediates into four
// consecutive 45-char slots (raws must be at least 186 bytes for the SIMD
// kernel's overhanging stores).
func extractCharsFlat36(flat *[4 * intermediateSz32]uint64, raws []byte) {
	if useAVX2 {
		_ = raws[185]
		digitsToChars36(&flat[0], &raws[0])
		return
	}
	for i, v := range flat {
		extractChars5(uint32(v), raws[5*i:5*i+5])
	}
}

// extractCharsFlat72 converts four 64-byte keys' 72 flat intermediates into
// four consecutive 90-char slots (raws must be at least 366 bytes). Two
// kernel calls; ascending order keeps overhanging stores harmless.
func extractCharsFlat72(flat *[4 * intermediateSz64]uint64, raws []byte) {
	if useAVX2 {
		_ = raws[365]
		digitsToChars36(&flat[0], &raws[0])
		digitsToChars36(&flat[36], &raws[180])
		return
	}
	for i, v := range flat {
		extractChars5(uint32(v), raws[5*i:5*i+5])
	}
}

// encode64Write renders the chars for encode64Parts output into out
// (length outLen). With AVX2 the chars render into a fixed-layout scratch
// (SIMD kernel + two scalar tail elements) and one copy places the trimmed
// window — measured faster than direct-position writes, which need six
// scalar element renders around the kernel.
func encode64Write(intermediate *[intermediateSz64]uint64, iz, dc, inZeros int, out []byte) {
	if !useAVX2 {
		encode64WriteGeneric(intermediate, iz, dc, inZeros, out)
		return
	}
	var raw [raw58Sz64]byte
	digitsToChars16Fast(&intermediate[0], &raw[0])
	extractChars5(uint32(intermediate[16]), raw[80:85])
	extractChars5(uint32(intermediate[17]), raw[85:90])
	copy(out, raw[raw58Sz64-len(out):])
}
