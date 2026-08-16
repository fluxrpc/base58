package base58

import (
	"encoding/binary"
	"errors"
	"math/bits"
)

var (
	ErrInvalidChar   = errors.New("base58: invalid base58 character")
	ErrInvalidLength = errors.New("base58: invalid encoded length")
	ErrValueTooLarge = errors.New("base58: decoded value too large for output size")
	ErrLeadingZeros  = errors.New("base58: leading '1' count does not match leading zero bytes")
)

// Decode decodes a base58 string to bytes. Each leading '1' in encoded
// produces a leading zero byte in the output. Empty input produces an empty
// (non-nil) slice.
//
// Encoded lengths matching a 32 or 64-byte representation — the common Solana
// sizes — are dispatched to the matrix-multiply fast paths (Decode32 /
// Decode64), which are ~10x faster than the long-multiplication fallback. A
// 32-byte value always encodes to 32-44 base58 chars; 64-byte to 64-88. The
// fast paths reject inputs whose natural byte count differs (via leading-zero
// validation), so we fall back to long multiplication on error.
func Decode(encoded string) ([]byte, error) {
	if len(encoded) == 0 {
		return []byte{}, nil
	}

	zeros := 0
	for zeros < len(encoded) && encoded[zeros] == '1' {
		zeros++
	}

	if zeros == len(encoded) {
		return make([]byte, zeros), nil
	}

	// A fast-path attempt can only succeed if the decoded byte count implied
	// by the significant char count k can equal 32 (resp. 64) minus the
	// leading-zero bytes. Check the exact bounds before paying for a full
	// matmul decode that would fail leading-zero validation.
	encLen := len(encoded)
	k := encLen - zeros
	if encLen >= 32 && encLen <= EncodedMaxLen32 {
		if need := 32 - zeros; need >= int(decMinBytes[k]) && need <= int(decMaxBytes[k]) {
			var dst [32]byte
			if err := Decode32(encoded, &dst); err == nil {
				out := make([]byte, 32)
				copy(out, dst[:])
				return out, nil
			}
		}
	}
	if encLen >= 64 && encLen <= EncodedMaxLen64 {
		if need := 64 - zeros; need >= int(decMinBytes[k]) && need <= int(decMaxBytes[k]) {
			var dst [64]byte
			if err := Decode64(encoded, &dst); err == nil {
				out := make([]byte, 64)
				copy(out, dst[:])
				return out, nil
			}
		}
	}

	return appendDecodeVariable(nil, encoded, zeros)
}

// AppendDecode appends the decoded bytes of encoded to dst and returns the
// extended buffer — the zero-allocation counterpart of Decode for callers
// that reuse buffers. With sufficient capacity in dst it does not allocate
// (except for decoded outputs larger than ~190 bytes, which use a heap limb
// buffer).
func AppendDecode(dst []byte, encoded string) ([]byte, error) {
	if len(encoded) == 0 {
		return dst, nil
	}

	zeros := 0
	for zeros < len(encoded) && encoded[zeros] == '1' {
		zeros++
	}

	if zeros == len(encoded) {
		for range zeros {
			dst = append(dst, 0)
		}
		return dst, nil
	}

	encLen := len(encoded)
	k := encLen - zeros
	if encLen >= 32 && encLen <= EncodedMaxLen32 {
		if need := 32 - zeros; need >= int(decMinBytes[k]) && need <= int(decMaxBytes[k]) {
			var tmp [32]byte
			if err := Decode32(encoded, &tmp); err == nil {
				return append(dst, tmp[:]...), nil
			}
		}
	}
	if encLen >= 64 && encLen <= EncodedMaxLen64 {
		if need := 64 - zeros; need >= int(decMinBytes[k]) && need <= int(decMaxBytes[k]) {
			var tmp [64]byte
			if err := Decode64(encoded, &tmp); err == nil {
				return append(dst, tmp[:]...), nil
			}
		}
	}

	return appendDecodeVariable(dst, encoded, zeros)
}

// pow58u64 holds 58^0 .. 58^10 for the digit-group multipliers.
var pow58u64 = [11]uint64{
	1, 58, 3364, 195112, 11316496, 656356768,
	38068692544, 2207984167552, 128063081718016,
	7427658739644928, 430804206899405824,
}

// m20Hi/m20Lo hold 58^20 as a two-word multiplier for the 20-char sweeps.
var m20Hi, m20Lo = bits.Mul64(pow58u64[10], pow58u64[10])

// appendDecodeVariable is a limb-based base58 decoder for inputs of
// arbitrary length; it appends the decoded bytes to dst (allocating exactly
// the required total when dst is nil). Characters are consumed most-significant-first in groups of ten
// (the leading group takes the length remainder); each group's value
// (< 58^10 < 2^59) is folded into a little-endian base-2^64 limb array with
// one 128-bit multiply-accumulate pass: work = work*58^len(group) + group.
// This does ~80x fewer inner-loop steps than byte-at-a-time
// work = work*58 + digit.
func appendDecodeVariable(dst []byte, encoded string, zeros int) ([]byte, error) {
	s := encoded[zeros:]
	n := len(s)

	// Upper bound on byte count of the non-leading-zero portion:
	// ceil(n * log(58)/log(256)) ~ n * 0.7322. Use 733/1000 + 1 for safety.
	maxBytes := n*733/1000 + 1
	numLimbs := (maxBytes + 7) / 8
	var limbArr [24]uint64 // decoded outputs up to ~190 bytes: no allocation
	var limbs []uint64     // little-endian: limbs[0] is least significant
	if numLimbs <= len(limbArr) {
		limbs = limbArr[:numLimbs]
	} else {
		limbs = make([]uint64, numLimbs)
	}

	top := 0 // number of active limbs
	i := 0
	groupLen := n % 20
	if groupLen == 0 {
		groupLen = 20
	}
	for i < n {
		// Build the group value g (< 58^20, two words) from groupLen chars.
		var gHi, gLo uint64
		var bad byte
		if groupLen == 20 {
			var half [2]uint64
			for h := range 2 {
				c := s[i : i+10] // one bounds check per half
				d0 := base58InverseFull[c[0]]
				d1 := base58InverseFull[c[1]]
				d2 := base58InverseFull[c[2]]
				d3 := base58InverseFull[c[3]]
				d4 := base58InverseFull[c[4]]
				d5 := base58InverseFull[c[5]]
				d6 := base58InverseFull[c[6]]
				d7 := base58InverseFull[c[7]]
				d8 := base58InverseFull[c[8]]
				d9 := base58InverseFull[c[9]]
				bad |= d0 | d1 | d2 | d3 | d4 | d5 | d6 | d7 | d8 | d9
				hi := uint32(d0)*11316496 + uint32(d1)*195112 +
					uint32(d2)*3364 + uint32(d3)*58 + uint32(d4)
				lo := uint32(d5)*11316496 + uint32(d6)*195112 +
					uint32(d7)*3364 + uint32(d8)*58 + uint32(d9)
				half[h] = uint64(hi)*656356768 + uint64(lo)
				i += 10
			}
			var c uint64
			gHi, gLo = bits.Mul64(half[0], pow58u64[10])
			gLo, c = bits.Add64(gLo, half[1], 0)
			gHi += c
		} else {
			var g1, g2 uint64
			l1 := 0
			if groupLen > 10 {
				l1 = groupLen - 10
			}
			for e := i + l1; i < e; i++ {
				d := base58InverseFull[s[i]]
				bad |= d
				g1 = g1*58 + uint64(d)
			}
			for e := i + groupLen - l1; i < e; i++ {
				d := base58InverseFull[s[i]]
				bad |= d
				g2 = g2*58 + uint64(d)
			}
			if l1 > 0 {
				var c uint64
				gHi, gLo = bits.Mul64(g1, pow58u64[10])
				gLo, c = bits.Add64(gLo, g2, 0)
				gHi += c
			} else {
				gLo = g2
			}
		}
		if bad >= 64 {
			return nil, ErrInvalidChar
		}

		// work = work*58^20 + g with a two-word multiplier and two-word
		// carry: per limb, two 128-bit multiplies and an add chain. The
		// top word of the product is < 2^54, so t2 cannot overflow.
		c0, c1 := gLo, gHi
		for j := 0; j < top; j++ {
			l := limbs[j]
			p1, p0 := bits.Mul64(l, m20Lo)
			q1, q0 := bits.Mul64(l, m20Hi)
			t0, k0 := bits.Add64(p0, c0, 0)
			s1, k1a := bits.Add64(p1, q0, k0)
			t1, k1b := bits.Add64(s1, c1, 0)
			limbs[j] = t0
			c0 = t1
			c1 = q1 + k1a + k1b
		}
		if c1 != 0 {
			limbs[top] = c0
			limbs[top+1] = c1
			top += 2
		} else if c0 != 0 {
			limbs[top] = c0
			top++
		}
		groupLen = 20
	}

	// s starts with a non-'1' char, so the value is non-zero and top >= 1.
	topBytes := (bits.Len64(limbs[top-1]) + 7) / 8
	start := len(dst)
	total := start + zeros + (top-1)*8 + topBytes
	if cap(dst) < total {
		grown := make([]byte, total)
		copy(grown, dst)
		out := grown
		dst = out
	} else {
		dst = dst[:total]
	}
	// The zeros prefix must be explicit: reused capacity may hold old data.
	for j := start; j < start+zeros; j++ {
		dst[j] = 0
	}
	p := total
	for j := 0; j < top-1; j++ {
		binary.BigEndian.PutUint64(dst[p-8:p], limbs[j])
		p -= 8
	}
	v := limbs[top-1]
	for k := 0; k < topBytes; k++ {
		p--
		dst[p] = byte(v)
		v >>= 8
	}
	return dst, nil
}

// Decode32 decodes a base58 string into a 32-byte array.
func Decode32(encoded string, dst *[32]byte) error {
	encLen := len(encoded)
	if encLen == 0 || encLen > raw58Sz32 {
		return ErrInvalidLength
	}

	// Build the base-58^5 groups directly from the string: raw58Sz32 is a
	// multiple of 5, so the string's 5-char tail groups align exactly with
	// the left-padded digit groups; the head group takes the remainder.
	var intermediate [intermediateSz32]uint64
	var bad byte
	var pairBad uint16
	i := 0
	head := encLen % 5
	if head == 0 {
		head = 5
	}
	var g uint32
	for ; i < head; i++ {
		d := base58InverseFull[encoded[i]]
		bad |= d
		g = g*58 + uint32(d)
	}
	intermediate[intermediateSz32-(encLen+4)/5] = uint64(g)
	for gi := intermediateSz32 - (encLen+4)/5 + 1; gi < intermediateSz32; gi++ {
		c := encoded[i : i+5] // one bounds check per group instead of five
		p0 := base58InversePairs[uint16(c[0])<<8|uint16(c[1])]
		p1 := base58InversePairs[uint16(c[2])<<8|uint16(c[3])]
		d4 := base58InverseFull[c[4]]
		pairBad |= p0 | p1 | uint16(d4)<<8
		intermediate[gi] = uint64(p0)*195112 +
			uint64(p1)*58 +
			uint64(d4)
		i += 5
	}
	if bad >= 64 || pairBad >= 0x8000 {
		return ErrInvalidChar
	}

	if !decode32Write(&intermediate, dst) {
		return ErrValueTooLarge
	}

	return validateLeadingZeros(encoded, dst[:])
}

// Decode64 decodes a base58 string into a 64-byte array.
func Decode64(encoded string, dst *[64]byte) error {
	encLen := len(encoded)
	if encLen == 0 || encLen > raw58Sz64 {
		return ErrInvalidLength
	}

	// Build the base-58^5 groups directly from the string (see Decode32).
	var intermediate [intermediateSz64]uint64
	var bad byte
	i := 0
	head := encLen % 5
	if head == 0 {
		head = 5
	}
	var g uint32
	for ; i < head; i++ {
		d := base58InverseFull[encoded[i]]
		bad |= d
		g = g*58 + uint32(d)
	}
	intermediate[intermediateSz64-(encLen+4)/5] = uint64(g)
	for gi := intermediateSz64 - (encLen+4)/5 + 1; gi < intermediateSz64; gi++ {
		c := encoded[i : i+5] // one bounds check per group instead of five
		d0 := base58InverseFull[c[0]]
		d1 := base58InverseFull[c[1]]
		d2 := base58InverseFull[c[2]]
		d3 := base58InverseFull[c[3]]
		d4 := base58InverseFull[c[4]]
		bad |= d0 | d1 | d2 | d3 | d4
		intermediate[gi] = uint64(d0)*11316496 +
			uint64(d1)*195112 +
			uint64(d2)*3364 +
			uint64(d3)*58 +
			uint64(d4)
		i += 5
	}
	if bad >= 64 {
		return ErrInvalidChar
	}

	if !decode64Write(&intermediate, dst) {
		return ErrValueTooLarge
	}

	return validateLeadingZeros(encoded, dst[:])
}

func decode32WriteSlow(intermediate *[intermediateSz32]uint64, dst *[32]byte) bool {
	var bin [binarySz32]uint64
	decodeMatMul32(intermediate, &bin)
	v := bin[binarySz32-1]
	for i := binarySz32 - 1; i >= 1; i-- {
		bin[i] = v & 0xFFFFFFFF
		v = bin[i-1] + (v >> 32)
	}
	bin[0] = v
	if bin[0] > 0xFFFFFFFF {
		return false
	}
	for i := 0; i < binarySz32; i += 2 {
		binary.BigEndian.PutUint64(dst[i*4:i*4+8], bin[i]<<32|bin[i+1])
	}
	return true
}

func decode64WriteSlow(intermediate *[intermediateSz64]uint64, dst *[64]byte) bool {
	var bin [binarySz64]uint64
	decodeMatMul64(intermediate, &bin)
	v := bin[binarySz64-1]
	for i := binarySz64 - 1; i >= 1; i-- {
		bin[i] = v & 0xFFFFFFFF
		v = bin[i-1] + (v >> 32)
	}
	bin[0] = v
	if bin[0] > 0xFFFFFFFF {
		return false
	}
	for i := 0; i < binarySz64; i += 2 {
		binary.BigEndian.PutUint64(dst[i*4:i*4+8], bin[i]<<32|bin[i+1])
	}
	return true
}

// validateLeadingZeros verifies that the number of leading '1' characters in
// the encoded input equals the number of leading zero bytes in the decoded
// output. This is a required invariant of base58: each leading zero byte in
// the raw value is represented by exactly one '1' in the encoding.
func validateLeadingZeros(encoded string, dst []byte) error {
	inLeading1s := 0
	for i := 0; i < len(encoded) && encoded[i] == '1'; i++ {
		inLeading1s++
	}

	outLeading0s := 0
	for _, b := range dst {
		if b != 0 {
			break
		}
		outLeading0s++
	}

	if inLeading1s != outLeading0s {
		return ErrLeadingZeros
	}
	return nil
}
