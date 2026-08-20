// Derived from Apache-2.0-licensed Firedancer/solana-go work and modified by
// AlphaBatem Labs. See LICENSE, LICENSE-MIT, and NOTICE.
package base58

import (
	"encoding/binary"
	"unsafe"
)

// Encode encodes a byte slice to a base58 string. Each leading zero byte in
// src produces a leading '1' in the output. Empty input produces an empty
// string.
//
// Inputs of exactly 32 or 64 bytes — the common Solana sizes (pubkey, hash,
// signature, private key) — are dispatched to dedicated matrix kernels.
// Other lengths use width-specific matrix or block-radix paths.
func Encode(buf []byte) string {
	switch len(buf) {
	case 0:
		return ""
	case 32:
		return Encode32((*[32]byte)(buf))
	case 64:
		return Encode64((*[64]byte)(buf))
	default:
		return encodeVariable(buf)
	}
}

// encodeVariableTail writes the base58 chars of b (which must be non-empty
// with no leading zero bytes) into the tail of out and returns the index of
// the first char. out must be sized by encodeVariableSize: each sweep writes
// an unconditional block of 10 digits.
//
// The input is packed into big-endian base-2^32 limbs; each pass divides the
// whole limb array by 58^5 (which fits in 32 bits), yielding five base58
// digits at once. Compared to byte-at-a-time long division this does ~20x
// fewer inner-loop steps (n/4 limbs per pass, 5 digits per pass), and the
// division by the constant 58^5 compiles to a multiply+shift.
func encodeVariableTail(b []byte, out []byte) int {
	nz := len(b)

	// Pack big-endian base-2^32 limbs. The first limb holds the leftover
	// 1-3 head bytes when nz is not a multiple of 4.
	numLimbs := (nz + 3) / 4
	var limbArr [64]uint32 // inputs up to 256 bytes need no limb allocation
	var limbs []uint32
	if numLimbs <= len(limbArr) {
		limbs = limbArr[:numLimbs]
	} else {
		limbs = make([]uint32, numLimbs)
	}
	i, k := 0, 0
	if head := nz & 3; head != 0 {
		var v uint32
		for ; i < head; i++ {
			v = v<<8 | uint32(b[i])
		}
		limbs[0] = v
		k = 1
	}
	for ; i < nz; i += 4 {
		limbs[k] = binary.BigEndian.Uint32(b[i : i+4])
		k++
	}

	p := len(out) // write cursor, moves down

	start := 0 // first limb that may be non-zero
	for {
		for start < numLimbs && limbs[start] == 0 {
			start++
		}
		if start == numLimbs {
			break
		}
		// Four chained divisions per limb visit: each subsequent chain
		// divides the previous chain's quotient in the same sweep,
		// yielding 20 digits per pass. The serial multiply+shift chains
		// interleave, so a sweep costs little more than a single pass.
		var rem1, rem2, rem3, rem4 uint64
		for j := start; j < numLimbs; j++ {
			v1 := rem1<<32 | uint64(limbs[j])
			q1 := v1 / r1div
			rem1 = v1 - q1*r1div
			v2 := rem2<<32 | q1
			q2 := v2 / r1div
			rem2 = v2 - q2*r1div
			v3 := rem3<<32 | q2
			q3 := v3 / r1div
			rem3 = v3 - q3*r1div
			v4 := rem4<<32 | q3
			q4 := v4 / r1div
			rem4 = v4 - q4*r1div
			limbs[j] = uint32(q4)
		}
		// rem1 holds the low 5 digits, then rem2, then rem3.
		r := uint32(rem1)
		q1 := r / 3364
		binary.LittleEndian.PutUint16(out[p-2:p], base58Pairs[r-q1*3364])
		q2 := q1 / 3364
		binary.LittleEndian.PutUint16(out[p-4:p-2], base58Pairs[q1-q2*3364])
		out[p-5] = base58Chars[q2]
		r = uint32(rem2)
		q1 = r / 3364
		binary.LittleEndian.PutUint16(out[p-7:p-5], base58Pairs[r-q1*3364])
		q2 = q1 / 3364
		binary.LittleEndian.PutUint16(out[p-9:p-7], base58Pairs[q1-q2*3364])
		out[p-10] = base58Chars[q2]
		r = uint32(rem3)
		q1 = r / 3364
		binary.LittleEndian.PutUint16(out[p-12:p-10], base58Pairs[r-q1*3364])
		q2 = q1 / 3364
		binary.LittleEndian.PutUint16(out[p-14:p-12], base58Pairs[q1-q2*3364])
		out[p-15] = base58Chars[q2]
		r = uint32(rem4)
		q1 = r / 3364
		binary.LittleEndian.PutUint16(out[p-17:p-15], base58Pairs[r-q1*3364])
		q2 = q1 / 3364
		binary.LittleEndian.PutUint16(out[p-19:p-17], base58Pairs[q1-q2*3364])
		out[p-20] = base58Chars[q2]
		p -= 20
	}

	// The value is non-zero, so its most significant digit is non-zero;
	// any leading '1' (digit 0) chars in the final pass are padding.
	for out[p] == '1' {
		p++
	}
	return p
}

// encodeVariableSize returns the multiple-of-20 buffer size
// encodeVariableTail requires for nz significant bytes (each sweep writes an
// unconditional block of 20 digits).
func encodeVariableSize(nz int) int {
	maxDigits := nz*138/100 + 1
	return (maxDigits + 19) / 20 * 20
}

// encodeSmallTail is encodeVariableTail for 1-8 significant bytes: the value
// fits a single uint64, so each 5-digit pass is one constant division.
func encodeSmallTail(b []byte, out []byte) int {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	p := len(out)
	for v >= r1div {
		q := v / r1div
		r := uint32(v - q*r1div)
		v = q
		q1 := r / 3364
		binary.LittleEndian.PutUint16(out[p-2:p], base58Pairs[r-q1*3364])
		q2 := q1 / 3364
		binary.LittleEndian.PutUint16(out[p-4:p-2], base58Pairs[q1-q2*3364])
		out[p-5] = base58Chars[q2]
		p -= 5
	}
	for r := uint32(v); r > 0; r /= 58 {
		p--
		out[p] = base58Chars[r%58]
	}
	return p
}

// encodeTail dispatches to the width-appropriate digit generator.
func encodeTail(b []byte, out []byte) int {
	if len(b) >= largeEncodeThreshold {
		return encodeLargeTail(b, out)
	}
	if len(b) <= 8 {
		return encodeSmallTail(b, out)
	}
	if len(b) <= 32 {
		return encodeSmallMatrixTail(b, out)
	}
	if len(b) <= 64 {
		return encodeMatrixTail(b, out)
	}
	return encodeBlockTail(b, out)
}

// encodeVariable is the string-returning limb-based encoder for inputs of
// arbitrary length.
func encodeVariable(bin []byte) string {
	binsz := len(bin)
	zcount := 0
	for zcount < binsz && bin[zcount] == 0 {
		zcount++
	}
	nz := binsz - zcount

	if nz == 0 {
		out := make([]byte, zcount)
		for i := range out {
			out[i] = '1'
		}
		return unsafe.String(unsafe.SliceData(out), len(out))
	}

	total := zcount + encodeVariableSize(nz)
	out := make([]byte, total)
	p := encodeTail(bin[zcount:], out)
	for range zcount {
		p--
		out[p] = '1'
	}
	res := out[p:]
	return unsafe.String(unsafe.SliceData(res), len(res))
}

// AppendEncode appends the base58 encoding of src to dst and returns the
// extended buffer. It is the zero-allocation counterpart of Encode: with a
// dst of sufficient capacity it does not allocate for inputs through roughly
// 1 KiB. Large inputs use math/big scratch to avoid quadratic block-Horner
// scaling. Exact 32 and 64-byte inputs dispatch to the dedicated matrix paths.
func AppendEncode(dst []byte, src []byte) []byte {
	switch len(src) {
	case 0:
		return dst
	case 32:
		return AppendEncode32(dst, (*[32]byte)(src))
	case 64:
		return AppendEncode64(dst, (*[64]byte)(src))
	}

	binsz := len(src)
	zcount := 0
	for zcount < binsz && src[zcount] == 0 {
		zcount++
	}
	nz := binsz - zcount

	for range zcount {
		dst = append(dst, '1')
	}
	if nz == 0 {
		return dst
	}

	size := encodeVariableSize(nz)
	start := len(dst)
	total := start + size
	if cap(dst) < total {
		grown := make([]byte, total)
		copy(grown, dst)
		dst = grown
	} else {
		dst = dst[:total]
	}
	p := encodeTail(src[zcount:], dst[start:])
	copy(dst[start:], dst[start+p:])
	return dst[:len(dst)-p]
}

// Encode32 encodes a 32-byte array to a base58 string.
//
// Allocates exactly one []byte of the encoded length. For zero-allocation
// hot paths, prefer AppendEncode32 which writes into a caller-owned buffer.
func Encode32(src *[32]byte) string {
	if out, ok := encode32Fast(src); ok {
		return unsafe.String(unsafe.SliceData(out), len(out))
	}
	var raw [raw58Buf32]byte
	outLen, skip := encode32Render(src, &raw)
	out := make([]byte, outLen)
	n := copy(out, raw[skip:raw58Sz32])
	for i := n; i < outLen; i++ {
		out[i] = '1' // all-zero input: nothing rendered, pad '1's
	}
	return unsafe.String(unsafe.SliceData(out), len(out))
}

// encode64Generic is the portable allocating Encode64 implementation.
func encode64Generic(src *[64]byte) string {
	var intermediate [intermediateSz64]uint64
	outLen, iz, dc, inZeros := encode64Parts(src, &intermediate)
	out := make([]byte, outLen)
	encode64Write(&intermediate, iz, dc, inZeros, out)
	return unsafe.String(unsafe.SliceData(out), len(out))
}

// appendEncode32Generic is the portable AppendEncode32 implementation.  The
// architecture wrappers select it directly or use it as their scalar fallback.
func appendEncode32Generic(dst []byte, src *[32]byte) []byte {
	var raw [raw58Buf32]byte
	outLen, skip := encode32Render(src, &raw)
	total := len(dst) + outLen
	if cap(dst) < total {
		grown := make([]byte, total)
		copy(grown, dst)
		dst = grown
	} else {
		dst = dst[:total]
	}
	out := dst[total-outLen:]
	n := copy(out, raw[skip:raw58Sz32])
	for i := n; i < outLen; i++ {
		out[i] = '1'
	}
	return dst
}

// AppendEncode64 appends the base58 encoding of src to dst and returns the
// extended buffer. It allocates only if dst has insufficient capacity.
func AppendEncode64(dst []byte, src *[64]byte) []byte {
	if out, ok := appendEncode64Fast(dst, src); ok {
		return out
	}
	var intermediate [intermediateSz64]uint64
	outLen, iz, dc, inZeros := encode64Parts(src, &intermediate)
	total := len(dst) + outLen
	if cap(dst) < total {
		grown := make([]byte, total)
		copy(grown, dst)
		dst = grown
	} else {
		dst = dst[:total]
	}
	encode64Write(&intermediate, iz, dc, inZeros, dst[total-outLen:])
	return dst
}

// encodeRaw32 fills raw with the base58 chars for a 32-byte input and
// returns the number of leading chars to skip when producing the final output
// (leading zero digits beyond one per leading zero byte).
func encodeRaw32(src *[32]byte, raw *[raw58Buf32]byte) int {
	var intermediate [intermediateSz32]uint64
	encodeMatMul32(src, &intermediate)

	// Carry propagation with the running value kept in a register: the
	// serial chain is add -> divide (multiply+shift) per step, without the
	// store-to-load forwarding penalty of updating the array in place.
	v := intermediate[intermediateSz32-1]
	for i := intermediateSz32 - 1; i >= 1; i-- {
		q := v / r1div
		intermediate[i] = v - q*r1div
		v = intermediate[i-1] + q
	}
	intermediate[0] = v

	extractChars32(&intermediate, raw)

	inLeading0s := 0
	for _, b := range src {
		if b != 0 {
			break
		}
		inLeading0s++
	}

	rawLeading0s := 0
	for _, b := range raw[:raw58Sz32] {
		if b != '1' {
			break
		}
		rawLeading0s++
	}

	return rawLeading0s - inLeading0s
}

// encodeRaw64 fills raw with the base58 chars for a 64-byte input and
// returns the number of leading chars to skip.
//
// The accumulation uses plain uint64 arithmetic. Each product is u32×u32 so
// it fits in u64. After the first 8 input limbs a mini-reduction prevents
// overflow before adding the remaining 8 limbs (matches Firedancer).
func encodeRaw64(src *[64]byte, raw *[raw58Sz64]byte) int {
	var intermediate [intermediateSz64]uint64
	encodeMatMul64(src, &intermediate)

	// Full carry propagation (register-carried; see encodeRaw32).
	v := intermediate[intermediateSz64-1]
	for i := intermediateSz64 - 1; i >= 1; i-- {
		q := v / r1div
		intermediate[i] = v - q*r1div
		v = intermediate[i-1] + q
	}
	intermediate[0] = v

	extractChars64(&intermediate, raw)

	inLeading0s := 0
	for _, b := range src {
		if b != 0 {
			break
		}
		inLeading0s++
	}

	rawLeading0s := 0
	for _, b := range raw {
		if b != '1' {
			break
		}
		rawLeading0s++
	}

	return rawLeading0s - inLeading0s
}

// extractChars32Generic converts the 9 base-58^5 values to 45 base58 chars.
// Fully unrolled: constant slice offsets need no bounds checks or index
// arithmetic, and the nine independent divide chains schedule freely.
func extractChars32Generic(intermediate *[intermediateSz32]uint64, raw *[raw58Buf32]byte) {
	extractChars5(uint32(intermediate[0]), raw[0:5])
	extractChars5(uint32(intermediate[1]), raw[5:10])
	extractChars5(uint32(intermediate[2]), raw[10:15])
	extractChars5(uint32(intermediate[3]), raw[15:20])
	extractChars5(uint32(intermediate[4]), raw[20:25])
	extractChars5(uint32(intermediate[5]), raw[25:30])
	extractChars5(uint32(intermediate[6]), raw[30:35])
	extractChars5(uint32(intermediate[7]), raw[35:40])
	extractChars5(uint32(intermediate[8]), raw[40:45])
}

// extractChars64Generic converts the 18 base-58^5 values to 90 base58 chars.
func extractChars64Generic(intermediate *[intermediateSz64]uint64, raw *[raw58Sz64]byte) {
	for i := range intermediateSz64 {
		extractChars5(uint32(intermediate[i]), raw[5*i:5*i+5])
	}
}

// extractChars5 writes the 5 base58 chars of v < 58^5, most significant
// first. Two chars per constant division via the base58Pairs LUT.
func extractChars5(v uint32, out []byte) {
	_ = out[4]
	q1 := v / 3364
	binary.LittleEndian.PutUint16(out[3:5], base58Pairs[v-q1*3364])
	q2 := q1 / 3364
	binary.LittleEndian.PutUint16(out[1:3], base58Pairs[q1-q2*3364])
	out[0] = base58Chars[q2]
}

// digitCount5 returns the number of significant base58 digits of v < 58^5
// (0 for v == 0).
func digitCount5(v uint32) int {
	switch {
	case v == 0:
		return 0
	case v < 58:
		return 1
	case v < 3364:
		return 2
	case v < 195112:
		return 3
	case v < 11316496:
		return 4
	}
	return 5
}

// encode32Parts fills intermediate and returns the exact encoded length plus
// the pieces needed to write the chars directly at their final positions:
// iz is the first non-zero intermediate element, dc its significant digit
// count, inZeros the count of leading zero bytes in src (leading '1's).
// encode32Render runs carry propagation with digit extraction fused into
// the chain: element i's chars render (into the fixed-layout raw buffer)
// the moment its remainder leaves the serial divide chain, overlapping the
// extraction latency with the rest of the chain. Returns the exact encoded
// length and the skip into raw. All-zero input reports outLen = leading
// zero count with skip = raw58Sz32 (nothing to copy; caller pads '1's).
func encode32Render(src *[32]byte, raw *[raw58Buf32]byte) (outLen, skip int) {
	var intermediate [intermediateSz32]uint64
	encodeMatMul32(src, &intermediate)

	iz, top := intermediateSz32, uint32(0)
	v := intermediate[intermediateSz32-1]
	for i := intermediateSz32 - 1; i >= 1; i-- {
		q := v / r1div
		r := uint32(v - q*r1div)
		extractChars5(r, raw[5*i:5*i+5])
		if r != 0 {
			iz, top = i, r
		}
		v = intermediate[i-1] + q
	}
	r0 := uint32(v)
	extractChars5(r0, raw[0:5])
	if r0 != 0 {
		iz, top = 0, r0
	}

	inZeros := 0
	for _, b := range src {
		if b != 0 {
			break
		}
		inZeros++
	}
	if iz == intermediateSz32 {
		return inZeros, raw58Sz32
	}
	dc := digitCount5(top)
	outLen = inZeros + dc + 5*(intermediateSz32-1-iz)
	return outLen, raw58Sz32 - outLen
}

// encode64Parts fills intermediate and returns the exact encoded length
// plus the pieces needed to write chars at their final positions (see
// encode32Parts).
func encode64Parts(src *[64]byte, intermediate *[intermediateSz64]uint64) (outLen, iz, dc, inZeros int) {
	encodeMatMul64(src, intermediate)

	v := intermediate[intermediateSz64-1]
	for i := intermediateSz64 - 1; i >= 1; i-- {
		q := v / r1div
		intermediate[i] = v - q*r1div
		v = intermediate[i-1] + q
	}
	intermediate[0] = v

	for _, b := range src {
		if b != 0 {
			break
		}
		inZeros++
	}
	for iz < intermediateSz64 && intermediate[iz] == 0 {
		iz++
	}
	if iz == intermediateSz64 {
		return inZeros, iz, 0, inZeros
	}
	dc = digitCount5(uint32(intermediate[iz]))
	outLen = inZeros + dc + 5*(intermediateSz64-1-iz)
	return
}

// encode64WriteGeneric renders the chars for encode64Parts output into out
// (length outLen), fully scalar.
func encode64WriteGeneric(intermediate *[intermediateSz64]uint64, iz, dc, inZeros int, out []byte) {
	for k := range inZeros {
		out[k] = '1'
	}
	if iz == intermediateSz64 {
		return
	}
	var tmp [5]byte
	extractChars5(uint32(intermediate[iz]), tmp[:])
	copy(out[inZeros:], tmp[5-dc:])
	pos := inZeros + dc
	for i := iz + 1; i < intermediateSz64; i++ {
		extractChars5(uint32(intermediate[i]), out[pos:pos+5])
		pos += 5
	}
}
