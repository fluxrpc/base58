package base58

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func BenchmarkBase58_AppendEncode32_x4(b *testing.B) {
	var srcs [4][32]byte
	for i := range srcs {
		for j := range srcs[i] {
			srcs[i][j] = byte(i*37 + j*11)
		}
	}
	var bufs [4][]byte
	for i := range bufs {
		bufs[i] = make([]byte, 0, EncodedMaxLen32)
	}
	b.SetBytes(128)
	for b.Loop() {
		bufs[0] = AppendEncode32(bufs[0][:0], &srcs[0])
		bufs[1] = AppendEncode32(bufs[1][:0], &srcs[1])
		bufs[2] = AppendEncode32(bufs[2][:0], &srcs[2])
		bufs[3] = AppendEncode32(bufs[3][:0], &srcs[3])
	}
}

func TestEncode32Batch(t *testing.T) {
	for _, n := range []int{0, 1, 3, 4, 5, 8, 17, 100} {
		srcs := make([][32]byte, n)
		for i := range srcs {
			rand.Read(srcs[i][:])
		}
		// Include leading-zero cases.
		if n > 2 {
			srcs[1] = [32]byte{}
			srcs[2][0] = 0
			srcs[2][1] = 0
		}
		got := Encode32Batch(srcs)
		assert.Len(t, got, n)
		for i := range srcs {
			assert.Equal(t, Encode32(&srcs[i]), got[i], "n=%d i=%d", n, i)
		}
	}
}

func BenchmarkBase58_Encode32Batch64(b *testing.B) {
	srcs := make([][32]byte, 64)
	for i := range srcs {
		rand.Read(srcs[i][:])
	}
	b.SetBytes(int64(64 * 32))
	for b.Loop() {
		Encode32Batch(srcs)
	}
}

func BenchmarkBase58_encodeRaw32x4(b *testing.B) {
	var srcs [4][32]byte
	for i := range srcs {
		rand.Read(srcs[i][:])
	}
	raws := make([]byte, 4*raw58Sz32+6)
	b.SetBytes(128)
	for b.Loop() {
		encodeRaw32x4(&srcs, raws)
	}
}

func BenchmarkBase58_encodeRaw32_x4single(b *testing.B) {
	var srcs [4][32]byte
	for i := range srcs {
		rand.Read(srcs[i][:])
	}
	var raws [4][raw58Buf32]byte
	b.SetBytes(128)
	for b.Loop() {
		for j := range 4 {
			encodeRaw32(&srcs[j], &raws[j])
		}
	}
}

func TestEncode64Batch(t *testing.T) {
	for _, n := range []int{0, 1, 3, 4, 5, 8, 17, 100} {
		srcs := make([][64]byte, n)
		for i := range srcs {
			rand.Read(srcs[i][:])
		}
		if n > 2 {
			srcs[1] = [64]byte{}
			srcs[2][0] = 0
			srcs[2][1] = 0
		}
		got := Encode64Batch(srcs)
		assert.Len(t, got, n)
		for i := range srcs {
			assert.Equal(t, Encode64(&srcs[i]), got[i], "n=%d i=%d", n, i)
		}
	}
}

func BenchmarkBase58_Encode64Batch64(b *testing.B) {
	srcs := make([][64]byte, 64)
	for i := range srcs {
		rand.Read(srcs[i][:])
	}
	b.SetBytes(int64(64 * 64))
	for b.Loop() {
		Encode64Batch(srcs)
	}
}
