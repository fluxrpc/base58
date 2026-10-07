//go:build amd64

package base58

import (
	"bytes"
	"crypto/rand"
	mrand "math/rand"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAVX2AvailableWhenRequired(t *testing.T) {
	if os.Getenv("BASE58_REQUIRE_AVX2") == "1" && !useAVX2 {
		t.Fatal("BASE58_REQUIRE_AVX2=1, but runtime AVX2 detection is false")
	}
}

func TestAppendEncode32_ScalarCapacityAndLeadingZeros(t *testing.T) {
	previous := useAVX2
	useAVX2 = false
	t.Cleanup(func() { useAVX2 = previous })
	testAppendEncode32CapacityAndLeadingZeros(t)
}

var append32GrowthResult []byte

func TestAppendEncode32_AVX2GrowthCapacity(t *testing.T) {
	if !useAVX2 {
		t.Skip("AVX2 not available")
	}
	for _, tc := range []struct {
		name string
		src  [32]byte
	}{
		{"zero", [32]byte{}},
		{"leadingZeros", [32]byte{31: 1}},
		{"43chars", [32]byte{1}},
		{"44chars", [32]byte{255}},
	} {
		for _, prefix := range []string{"", "pubkey="} {
			t.Run(tc.name+"/"+prefix, func(t *testing.T) {
				dst := make([]byte, len(prefix))
				copy(dst, prefix)
				want := prefix + encodeDivisionReference(tc.src[:])
				got := AppendEncode32(dst, &tc.src)
				if string(got) != want {
					t.Fatalf("got %q, want %q", got, want)
				}
				if cap(got) != len(prefix)+EncodedMaxLen32 {
					t.Fatalf("capacity = %d, want %d", cap(got), len(prefix)+EncodedMaxLen32)
				}
				wantAllocs := 1.0
				if len(want)-len(prefix) == EncodedMaxLen32 {
					wantAllocs = 2 // The maximum-length encoding leaves no spare byte.
				}
				allocs := testing.AllocsPerRun(100, func() {
					out := AppendEncode32(dst, &tc.src)
					append32GrowthResult = append(out, '!')
				})
				if allocs != wantAllocs {
					t.Fatalf("encode plus suffix allocated %g times, want %g", allocs, wantAllocs)
				}
				if string(append32GrowthResult) != want+"!" {
					t.Fatalf("got %q, want %q", append32GrowthResult, want+"!")
				}
			})
		}
	}
}

// TestScalarFallback_Matches runs the fixed-size paths with the AVX2 kernels
// disabled and cross-checks results against the default configuration, so the
// scalar assembly stays covered on AVX2 machines.
func TestScalarFallback_Matches(t *testing.T) {
	if !useAVX2 {
		t.Skip("AVX2 not available; scalar path is already the default")
	}
	defer func() { useAVX2 = true }()

	for range 2000 {
		var src64 [64]byte
		rand.Read(src64[:])
		src32 := (*[32]byte)(src64[:32])

		useAVX2 = true
		enc32 := Encode32(src32)
		enc64 := Encode64(&src64)

		useAVX2 = false
		assert.Equal(t, enc32, Encode32(src32))
		assert.Equal(t, enc64, Encode64(&src64))

		var dst32 [32]byte
		var dst64 [64]byte
		require.NoError(t, Decode32(enc32, &dst32))
		require.NoError(t, Decode64(enc64, &dst64))
		assert.Equal(t, *src32, dst32)
		assert.Equal(t, src64, dst64)
	}

	// The variable-width encoder has separate AVX2 and portable convolution
	// paths once it starts absorbing 64-byte blocks. Keep them bit-identical.
	rng := mrand.New(mrand.NewSource(2))
	for _, size := range []int{9, 12, 16, 31, 33, 48, 63, 65, 100, 128, 256, 1000} {
		for range 50 {
			src := make([]byte, size)
			_, _ = rng.Read(src)
			useAVX2 = true
			want := Encode(src)
			useAVX2 = false
			assert.Equal(t, want, Encode(src), "size=%d", size)
		}
	}
}

func TestDigitsToChars8_Direct(t *testing.T) {
	if !useAVX2 {
		t.Skip("no AVX2")
	}
	rng := mrand.New(mrand.NewSource(1))
	for iter := 0; iter < 1000; iter++ {
		var v [8]uint64
		for i := range v {
			v[i] = uint64(rng.Int63n(656356768))
		}
		var got [48]byte
		digitsToChars8(&v[0], &got[0])

		var want [40]byte
		for i := range v {
			extractChars5(uint32(v[i]), want[5*i:5*i+5])
		}
		if !bytes.Equal(got[:40], want[:]) {
			t.Fatalf("iter %d\n v=%v\n got  %q\n want %q", iter, v, got[:40], want[:])
		}
	}
}
