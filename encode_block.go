// Block-radix performance work informed by base58-turbo.
// Copyright 2026 AlphaBatem Labs.
// SPDX-License-Identifier: MIT OR Apache-2.0

package base58

import "encoding/binary"

// pow2_512_base58_5 is 2^512 represented as little-endian base-58^5 limbs.
// It is the multiplier used when a 64-byte block is appended to an existing
// base58 bignum: state = state*2^512 + block.
var pow2_512_base58_5 = [...]uint32{
	114698272, 475011419, 135769242, 494394213, 526051483, 437394159,
	19598664, 243715433, 337219057, 222507322, 211243284, 450400380,
	416249884, 213548060, 82779834, 283734095, 542475966, 17217,
}

// encodeBlockTail converts a non-zero, zero-stripped byte string to base58.
// Unlike encodeVariableTail, which repeatedly divides the whole binary value,
// this path consumes 64-byte blocks with a radix tree. Each block is converted
// by the fixed-width matrix kernel and folded into a little-endian base-58^5
// bignum with a short Comba multiplication.
func encodeBlockTail(src, out []byte) int {
	// Each complete 64-byte block occupies at most 18 base-58^5 limbs. The
	// head estimate covers the scalar prefix; this bound includes transient
	// leading zero limbs before the post-fold trim.
	need := (len(src)/64)*18 + ((len(src)%64)*138/100+5)/5
	switch {
	case need <= 90:
		var digitStore [107]uint32 // 17 zero limbs pad the vector convolution
		var tmp [90]uint64
		return encodeBlockTailScratch(src, out, digitStore[17:17+need], tmp[:need])
	case need <= 288:
		var digitStore [305]uint32
		var tmp [288]uint64
		return encodeBlockTailScratch(src, out, digitStore[17:17+need], tmp[:need])
	default:
		digitStore := make([]uint32, need+17)
		tmp := make([]uint64, need)
		return encodeBlockTailScratch(src, out, digitStore[17:], tmp)
	}
}

// encodeMatrixTail uses the existing fixed-width matrix kernels for every
// significant input below 64 bytes. Right-aligning into a zero-padded 32- or
// 64-byte value preserves the numeric value; explicit leading zero bytes have
// already been removed by the caller and are handled separately.
func encodeMatrixTail(src, out []byte) int {
	if p, ok := encodePaddedFixedTail(src, out); ok {
		return p
	}
	var digits [intermediateSz64]uint32
	var count int
	if len(src) <= 32 {
		var padded [32]byte
		copy(padded[32-len(src):], src)
		count = encodeBlock32(&padded, digits[:])
	} else {
		var padded [64]byte
		copy(padded[64-len(src):], src)
		count = encodeBlock64(&padded, digits[:])
	}
	return encodeDigits5Tail(digits[:count], out)
}

// encodeSmallMatrixTail evaluates the bottom-right submatrix of encTable32.
// A W-word value occupies W+1 base-58^5 accumulator slots (including carry),
// so inputs from 9 through 32 bytes avoid the repeated whole-value division
// used by encodeVariableTail.
func encodeSmallMatrixTail(src, out []byte) int {
	w := (len(src) + 3) / 4
	if w == 4 {
		return encodeSmallMatrix4Tail(src, out)
	}
	var words [binarySz32]uint32
	head := len(src) - 4*(w-1)
	var top uint32
	for _, b := range src[:head] {
		top = top<<8 | uint32(b)
	}
	words[0] = top
	pos := head
	for i := 1; i < w; i++ {
		words[i] = binary.BigEndian.Uint32(src[pos : pos+4])
		pos += 4
	}

	var acc [intermediateSz32]uint64
	rowBase := binarySz32 - w
	colBase := intermediateSz32 - 1 - w
	for row := 0; row < w; row++ {
		x := uint64(words[row])
		tableRow := &encTable32[rowBase+row]
		for col := 0; col < w; col++ {
			acc[col+1] += x * uint64(tableRow[colBase+col])
		}
	}

	v := acc[w]
	var digits [intermediateSz32]uint32
	for i := w; i >= 1; i-- {
		q := v / r1div
		digits[w-i] = uint32(v - q*r1div)
		v = acc[i-1] + q
	}
	digits[w] = uint32(v)
	count := w + 1
	for count > 1 && digits[count-1] == 0 {
		count--
	}
	return encodeDigits5Tail(digits[:count], out)
}

func encodeSmallMatrix4Tail(src, out []byte) int {
	head := len(src) - 12
	var x0 uint32
	for _, b := range src[:head] {
		x0 = x0<<8 | uint32(b)
	}
	x1 := binary.BigEndian.Uint32(src[head : head+4])
	x2 := binary.BigEndian.Uint32(src[head+4 : head+8])
	x3 := binary.BigEndian.Uint32(src[head+8 : head+12])

	// Bottom-right 4x4 triangle of encTable32, with one leading carry slot.
	a1 := uint64(x0) * 280
	a2 := uint64(x0)*127692781 + uint64(x1)*42
	a3 := uint64(x0)*389432875 + uint64(x1)*537767569 + uint64(x2)*6
	a4 := uint64(x0)*357132832 + uint64(x1)*410450016 + uint64(x2)*356826688 + uint64(x3)

	var digits [5]uint32
	v := a4
	q := v / r1div
	digits[0] = uint32(v - q*r1div)
	v = a3 + q
	q = v / r1div
	digits[1] = uint32(v - q*r1div)
	v = a2 + q
	q = v / r1div
	digits[2] = uint32(v - q*r1div)
	v = a1 + q
	q = v / r1div
	digits[3] = uint32(v - q*r1div)
	digits[4] = uint32(q)
	count := 5
	if digits[4] == 0 {
		count = 4
	}
	return encodeDigits5Tail(digits[:count], out)
}

func encodeBlockTailScratch(src, out []byte, digits []uint32, tmp []uint64) int {
	count := 1
	var blocks []byte

	if len(src) >= 64 {
		blockCount := len(src) / 64
		head := len(src) % 64
		if blockCount >= 2 || head >= 16 {
			prefix, rest := src[:head], src[head:]
			if head == 0 {
				count = encodeBlock64((*[64]byte)(rest[:64]), digits)
				rest = rest[64:]
			}
			src, blocks = prefix, rest
		} else {
			// For one block plus a short tail, seeding from the block avoids a
			// full 18-limb multiply and the tail is cheap to append directly.
			count = encodeBlock64((*[64]byte)(src[:64]), digits)
			src = src[64:]
		}
	}

	if len(src) >= 32 {
		count = encodeBlock32((*[32]byte)(src[:32]), digits)
		src = src[32:]
	}

	chunks := src
	for len(chunks) >= 4 {
		encodeHornerStep(digits, &count, 1<<32, uint64(binary.BigEndian.Uint32(chunks[:4])))
		chunks = chunks[4:]
	}
	if len(chunks) != 0 {
		var chunk uint64
		for _, b := range chunks {
			chunk = chunk<<8 | uint64(b)
		}
		encodeHornerStep(digits, &count, uint64(1)<<(8*len(chunks)), chunk)
	}

	if len(blocks) != 0 {
		count = encodeAbsorbBlocks(blocks, digits, tmp, count)
	}

	return encodeDigits5Tail(digits[:count], out)
}

func encodeDigits5Tail(digits []uint32, out []byte) int {
	p := len(out)
	for _, digit := range digits {
		p -= 5
		extractChars5(digit, out[p:p+5])
	}
	for out[p] == '1' {
		p++
	}
	return p
}

func encodeBlock32(src *[32]byte, digits []uint32) int {
	var intermediate [intermediateSz32]uint64
	encodeMatMul32(src, &intermediate)
	v := intermediate[intermediateSz32-1]
	for i := intermediateSz32 - 1; i >= 1; i-- {
		q := v / r1div
		digits[intermediateSz32-1-i] = uint32(v - q*r1div)
		v = intermediate[i-1] + q
	}
	digits[intermediateSz32-1] = uint32(v)
	count := intermediateSz32
	for count > 1 && digits[count-1] == 0 {
		count--
	}
	return count
}

func encodeBlock64(src *[64]byte, digits []uint32) int {
	var intermediate [intermediateSz64]uint64
	encodeMatMul64(src, &intermediate)
	v := intermediate[intermediateSz64-1]
	for i := intermediateSz64 - 1; i >= 1; i-- {
		q := v / r1div
		digits[intermediateSz64-1-i] = uint32(v - q*r1div)
		v = intermediate[i-1] + q
	}
	digits[intermediateSz64-1] = uint32(v)
	count := intermediateSz64
	for count > 1 && digits[count-1] == 0 {
		count--
	}
	return count
}

func encodeHornerStep(digits []uint32, count *int, multiplier, chunk uint64) {
	carry := chunk
	for i := 0; i < *count; i++ {
		v := uint64(digits[i])*multiplier + carry
		q := v / r1div
		digits[i] = uint32(v - q*r1div)
		carry = q
	}
	for carry != 0 {
		q := carry / r1div
		digits[*count] = uint32(carry - q*r1div)
		carry = q
		*count++
	}
}

func encodeAbsorbBlocks(blocks []byte, digits []uint32, tmp []uint64, count int) int {
	var blockDigits [intermediateSz64]uint32
	for len(blocks) >= 64 {
		block := (*[64]byte)(blocks[:64])
		blockCount := encodeBlock64(block, blockDigits[:])
		n := count + len(pow2_512_base58_5)
		if encodeConvolveP512All(digits, count, n, tmp) {
			// Filled by the architecture-specific vector kernel.
		} else if count < 96 {
			clear(tmp[:n])
			for i, digit := range digits[:count] {
				a := uint64(digit)
				for j, weight := range pow2_512_base58_5 {
					tmp[i+j] += a * uint64(weight)
				}
			}
		} else {
			// Column-major Comba multiplication. The middle columns always
			// contain exactly 18 terms; spelling that dot product out lets the
			// compiler schedule independent multiplies without loop overhead.
			for column := 0; column < 17; column++ {
				var sum uint64
				for k := 0; k <= column; k++ {
					sum += uint64(digits[k]) * uint64(pow2_512_base58_5[column-k])
				}
				tmp[column] = sum
			}
			encodeConvolveP512(digits, count, tmp)
			for column := count; column < n; column++ {
				var sum uint64
				for k := column - 17; k < count; k++ {
					sum += uint64(digits[k]) * uint64(pow2_512_base58_5[column-k])
				}
				tmp[column] = sum
			}
		}
		for i, digit := range blockDigits[:blockCount] {
			tmp[i] += uint64(digit)
		}

		encodeReduceColumns(tmp, digits, n)
		count = n
		for count > 1 && digits[count-1] == 0 {
			count--
		}
		blocks = blocks[64:]
	}
	return count
}

func encodeMulP512(d []uint32) uint64 {
	_ = d[17]
	return uint64(d[0])*uint64(pow2_512_base58_5[17]) +
		uint64(d[1])*uint64(pow2_512_base58_5[16]) +
		uint64(d[2])*uint64(pow2_512_base58_5[15]) +
		uint64(d[3])*uint64(pow2_512_base58_5[14]) +
		uint64(d[4])*uint64(pow2_512_base58_5[13]) +
		uint64(d[5])*uint64(pow2_512_base58_5[12]) +
		uint64(d[6])*uint64(pow2_512_base58_5[11]) +
		uint64(d[7])*uint64(pow2_512_base58_5[10]) +
		uint64(d[8])*uint64(pow2_512_base58_5[9]) +
		uint64(d[9])*uint64(pow2_512_base58_5[8]) +
		uint64(d[10])*uint64(pow2_512_base58_5[7]) +
		uint64(d[11])*uint64(pow2_512_base58_5[6]) +
		uint64(d[12])*uint64(pow2_512_base58_5[5]) +
		uint64(d[13])*uint64(pow2_512_base58_5[4]) +
		uint64(d[14])*uint64(pow2_512_base58_5[3]) +
		uint64(d[15])*uint64(pow2_512_base58_5[2]) +
		uint64(d[16])*uint64(pow2_512_base58_5[1]) +
		uint64(d[17])*uint64(pow2_512_base58_5[0])
}

func encodeReduceColumns(tmp []uint64, digits []uint32, count int) {
	var carry uint64
	for i, column := range tmp[:count] {
		v := column + carry
		q := v / r1div
		digits[i] = uint32(v - q*r1div)
		carry = q
	}
}
