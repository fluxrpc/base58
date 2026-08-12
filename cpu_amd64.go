//go:build amd64

package base58

// useAVX2 gates the AVX2 matmul kernels. Requires AVX2 CPU support plus
// OS-enabled YMM state (OSXSAVE + XCR0).
var useAVX2 = x86HasAVX2()

func x86HasAVX2() bool
