//go:build arm64

package base58

// Only the 32-byte matrix multiply has an assembly path; the 64-byte
// path uses extended-precision arithmetic via math/bits which the Go
// compiler lowers to optimal UMULH/ADCS sequences.

//go:noescape
func encodeMatMul32(src *[32]byte, intermediate *[intermediateSz32]uint64)

//go:noescape
func decodeMatMul32(intermediate *[intermediateSz32]uint64, bin *[binarySz32]uint64)
