//go:build !arm64 && !amd64

package base58

import "encoding/binary"

func encodeMatMul32(src *[32]byte, intermediate *[intermediateSz32]uint64) {
	var bin [binarySz32]uint32
	for i := range binarySz32 {
		bin[i] = binary.BigEndian.Uint32(src[i*4 : i*4+4])
	}
	for i := range binarySz32 {
		for k := range intermediateSz32 - 1 {
			intermediate[k+1] += uint64(bin[i]) * uint64(encTable32[i][k])
		}
	}
}

func decodeMatMul32(intermediate *[intermediateSz32]uint64, bin *[binarySz32]uint64) {
	for i := range intermediateSz32 {
		for k := range binarySz32 {
			bin[k] += intermediate[i] * uint64(decTable32[i][k])
		}
	}
}
