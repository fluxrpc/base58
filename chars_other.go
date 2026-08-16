//go:build !amd64

package base58

func appendEncode64Fast(dst []byte, src *[64]byte) ([]byte, bool) {
	return nil, false
}

func extractChars32(intermediate *[intermediateSz32]uint64, raw *[raw58Buf32]byte) {
	extractChars32Generic(intermediate, raw)
}

func extractChars64(intermediate *[intermediateSz64]uint64, raw *[raw58Sz64]byte) {
	extractChars64Generic(intermediate, raw)
}

func extractCharsFlat36(flat *[4 * intermediateSz32]uint64, raws []byte) {
	for i, v := range flat {
		extractChars5(uint32(v), raws[5*i:5*i+5])
	}
}

func extractCharsFlat72(flat *[4 * intermediateSz64]uint64, raws []byte) {
	for i, v := range flat {
		extractChars5(uint32(v), raws[5*i:5*i+5])
	}
}

func encode64Write(intermediate *[intermediateSz64]uint64, iz, dc, inZeros int, out []byte) {
	encode64WriteGeneric(intermediate, iz, dc, inZeros, out)
}
