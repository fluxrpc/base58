package base58

import "unsafe"

// Encode32Batch encodes a slice of 32-byte values (e.g. Solana account keys)
// and returns their base58 strings. All strings share one backing buffer, so
// the whole batch costs two allocations regardless of length.
//
// Batches are processed four at a time: the four serial carry-propagation
// chains — the latency floor of a single encode — interleave in one loop, so
// per-key cost drops well below four independent Encode32 calls.
func Encode32Batch(srcs [][32]byte) []string {
	n := len(srcs)
	out := make([]string, n)
	if n == 0 {
		return out
	}
	// Each key gets a fixed 45-char slot written in place; the returned
	// string skips the slot's leading padding. +6 tail bytes keep the SIMD
	// kernel's overhanging stores inside the buffer.
	backing := make([]byte, n*raw58Sz32+6)

	i := 0
	for ; i+4 <= n; i += 4 {
		encodeRaw32x4((*[4][32]byte)(srcs[i:i+4]), backing[i*raw58Sz32:])
		for j := range 4 {
			slot := backing[(i+j)*raw58Sz32 : (i+j+1)*raw58Sz32]
			skip := rawSkip32(&srcs[i+j], slot)
			seg := slot[skip:]
			out[i+j] = unsafe.String(unsafe.SliceData(seg), len(seg))
		}
	}
	for ; i < n; i++ {
		raw := (*[raw58Buf32]byte)(backing[i*raw58Sz32:])
		skip := encodeRaw32(&srcs[i], raw)
		seg := backing[i*raw58Sz32+skip : (i+1)*raw58Sz32]
		out[i] = unsafe.String(unsafe.SliceData(seg), len(seg))
	}
	return out
}

// encodeRaw32x4 runs four 32-byte encodes with their carry-propagation
// chains interleaved, writing the chars into four consecutive 45-byte slots
// of raws (which must be at least 4*45+6 bytes for the SIMD extraction's
// overhanging stores; ascending write order keeps the results intact).
// Each carry chain alone is a ~8-step serial dependency on a multiply-based
// constant division; interleaving four lets the CPU overlap them, which a
// sequence of independent Encode32 calls does not achieve.
func encodeRaw32x4(srcs *[4][32]byte, raws []byte) {
	var flat [4 * intermediateSz32]uint64
	ia := (*[intermediateSz32]uint64)(flat[0:])
	ib := (*[intermediateSz32]uint64)(flat[intermediateSz32:])
	ic := (*[intermediateSz32]uint64)(flat[2*intermediateSz32:])
	id := (*[intermediateSz32]uint64)(flat[3*intermediateSz32:])
	encodeMatMul32(&srcs[0], ia)
	encodeMatMul32(&srcs[1], ib)
	encodeMatMul32(&srcs[2], ic)
	encodeMatMul32(&srcs[3], id)

	va, vb, vc, vd := ia[intermediateSz32-1], ib[intermediateSz32-1], ic[intermediateSz32-1], id[intermediateSz32-1]
	for i := intermediateSz32 - 1; i >= 1; i-- {
		qa := va / r1div
		ia[i] = va - qa*r1div
		va = ia[i-1] + qa
		qb := vb / r1div
		ib[i] = vb - qb*r1div
		vb = ib[i-1] + qb
		qc := vc / r1div
		ic[i] = vc - qc*r1div
		vc = ic[i-1] + qc
		qd := vd / r1div
		id[i] = vd - qd*r1div
		vd = id[i-1] + qd
	}
	ia[0], ib[0], ic[0], id[0] = va, vb, vc, vd

	extractCharsFlat36(&flat, raws)
}

// Encode64Batch encodes a slice of 64-byte values (e.g. Solana signatures)
// and returns their base58 strings. All strings share one backing buffer, so
// the whole batch costs two allocations regardless of length. Keys are
// processed four at a time with interleaved carry-propagation chains, like
// Encode32Batch.
func Encode64Batch(srcs [][64]byte) []string {
	n := len(srcs)
	out := make([]string, n)
	if n == 0 {
		return out
	}
	backing := make([]byte, n*raw58Sz64+6)

	i := 0
	for ; i+4 <= n; i += 4 {
		encodeRaw64x4((*[4][64]byte)(srcs[i:i+4]), backing[i*raw58Sz64:])
		for j := range 4 {
			slot := backing[(i+j)*raw58Sz64 : (i+j+1)*raw58Sz64]
			skip := rawSkip64(&srcs[i+j], slot)
			seg := slot[skip:]
			out[i+j] = unsafe.String(unsafe.SliceData(seg), len(seg))
		}
	}
	for ; i < n; i++ {
		raw := (*[raw58Sz64]byte)(backing[i*raw58Sz64:])
		skip := encodeRaw64(&srcs[i], raw)
		seg := backing[i*raw58Sz64+skip : (i+1)*raw58Sz64]
		out[i] = unsafe.String(unsafe.SliceData(seg), len(seg))
	}
	return out
}

// encodeRaw64x4 runs four 64-byte encodes with interleaved carry chains,
// writing chars into four consecutive 90-byte slots of raws (which must be
// at least 4*90+6 bytes).
func encodeRaw64x4(srcs *[4][64]byte, raws []byte) {
	var flat [4 * intermediateSz64]uint64
	ia := (*[intermediateSz64]uint64)(flat[0:])
	ib := (*[intermediateSz64]uint64)(flat[intermediateSz64:])
	ic := (*[intermediateSz64]uint64)(flat[2*intermediateSz64:])
	id := (*[intermediateSz64]uint64)(flat[3*intermediateSz64:])
	encodeMatMul64(&srcs[0], ia)
	encodeMatMul64(&srcs[1], ib)
	encodeMatMul64(&srcs[2], ic)
	encodeMatMul64(&srcs[3], id)

	va, vb, vc, vd := ia[intermediateSz64-1], ib[intermediateSz64-1], ic[intermediateSz64-1], id[intermediateSz64-1]
	for i := intermediateSz64 - 1; i >= 1; i-- {
		qa := va / r1div
		ia[i] = va - qa*r1div
		va = ia[i-1] + qa
		qb := vb / r1div
		ib[i] = vb - qb*r1div
		vb = ib[i-1] + qb
		qc := vc / r1div
		ic[i] = vc - qc*r1div
		vc = ic[i-1] + qc
		qd := vd / r1div
		id[i] = vd - qd*r1div
		vd = id[i-1] + qd
	}
	ia[0], ib[0], ic[0], id[0] = va, vb, vc, vd

	extractCharsFlat72(&flat, raws)
}

// rawSkip64 computes the number of leading chars to drop from a 90-char
// slot: leading zero digits beyond one '1' per leading zero byte.
func rawSkip64(src *[64]byte, raw []byte) int {
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

// rawSkip32 computes the number of leading chars to drop from a 45-char
// slot: leading zero digits beyond one '1' per leading zero byte.
func rawSkip32(src *[32]byte, raw []byte) int {
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
