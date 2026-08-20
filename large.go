// Copyright 2026 AlphaBatem Labs.
// SPDX-License-Identifier: MIT OR Apache-2.0

package base58

import "math/big"

const (
	largeEncodeThreshold = 32 << 10
	largeDecodeThreshold = 8 << 10
	largeDecodeLeafChars = 1024
)

var largePow58 = func() [11]big.Int {
	var powers [11]big.Int
	for i, value := range pow58u64 {
		powers[i].SetUint64(value)
	}
	return powers
}()

// encodeLargeTail uses math/big's divide-and-conquer base conversion. Its
// higher setup/allocation cost loses at ordinary payload sizes but avoids the
// block Horner path's quadratic wall for inputs above tens of KiB.
func encodeLargeTail(src, out []byte) int {
	var value big.Int
	value.SetBytes(src)
	digits := value.Append(out[:0], 58)
	for i, c := range digits {
		var digit byte
		switch {
		case c <= '9':
			digit = c - '0'
		case c >= 'a':
			digit = c - 'a' + 10
		default:
			digit = c - 'A' + 36
		}
		digits[i] = base58Chars[digit]
	}
	p := len(out) - len(digits)
	copy(out[p:], digits)
	return p
}

func appendDecodeLarge(dst []byte, encoded string, zeros int) ([]byte, error) {
	var bad byte
	for i := zeros; i < len(encoded); i++ {
		bad |= base58InverseFull[encoded[i]]
	}
	if bad >= 64 {
		return nil, ErrInvalidChar
	}

	powers := make(map[int]*big.Int, 16)
	value, ok := decodeLargeTree(encoded[zeros:], powers)
	if !ok {
		return nil, ErrInvalidChar
	}

	start := len(dst)
	payloadBytes := (value.BitLen() + 7) / 8
	total := start + zeros + payloadBytes
	if cap(dst) < total {
		grown := make([]byte, total)
		copy(grown, dst)
		dst = grown
	} else {
		dst = dst[:total]
		clear(dst[start : start+zeros])
	}
	value.FillBytes(dst[start+zeros:])
	return dst, nil
}

// decodeLargeTree recursively combines bounded Horner leaves. math/big's
// sub-quadratic multiplication then handles the growing operands, instead of
// sweeping the complete limb array for every 20 input characters.
func decodeLargeTree(src string, powers map[int]*big.Int) (*big.Int, bool) {
	if len(src) <= largeDecodeLeafChars {
		return decodeLargeLeaf(src)
	}

	split := len(src) / 2
	split -= split % 10
	left, ok := decodeLargeTree(src[:split], powers)
	if !ok {
		return nil, false
	}
	right, ok := decodeLargeTree(src[split:], powers)
	if !ok {
		return nil, false
	}

	rightLen := len(src) - split
	power := largePower58(rightLen, powers)
	left.Mul(left, power)
	left.Add(left, right)
	return left, true
}

func largePower58(exponent int, powers map[int]*big.Int) *big.Int {
	if exponent <= 10 {
		return &largePow58[exponent]
	}
	if power := powers[exponent]; power != nil {
		return power
	}
	leftExponent := exponent / 2
	power := new(big.Int).Mul(
		largePower58(leftExponent, powers),
		largePower58(exponent-leftExponent, powers),
	)
	powers[exponent] = power
	return power
}

func decodeLargeLeaf(src string) (*big.Int, bool) {
	value := new(big.Int)
	var addend big.Int
	i := 0
	groupLen := len(src) % 10
	if groupLen == 0 {
		groupLen = 10
	}
	for i < len(src) {
		group, ok := decodeLargeGroup(src[i : i+groupLen])
		if !ok {
			return nil, false
		}
		value.Mul(value, &largePow58[groupLen])
		value.Add(value, addend.SetUint64(group))
		i += groupLen
		groupLen = 10
	}
	return value, true
}

func decodeLargeGroup(src string) (uint64, bool) {
	var value uint64
	var bad byte
	for i := range src {
		digit := base58InverseFull[src[i]]
		bad |= digit
		value = value*58 + uint64(digit)
	}
	return value, bad < 64
}
