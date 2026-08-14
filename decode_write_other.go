//go:build !amd64

package base58

func decode32Write(intermediate *[intermediateSz32]uint64, dst *[32]byte) bool {
	return decode32WriteSlow(intermediate, dst)
}

func decode64Write(intermediate *[intermediateSz64]uint64, dst *[64]byte) bool {
	return decode64WriteSlow(intermediate, dst)
}
