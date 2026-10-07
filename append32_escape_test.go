package base58_test

import (
	"runtime"
	"testing"

	"github.com/fluxrpc/base58"
)

// Exercise the public API across a package boundary, as value-receiver
// public-key methods do in callers of this package.
type publicKey [32]byte

func (p publicKey) MarshalJSON() ([]byte, error) {
	buf := make([]byte, 0, base58.EncodedMaxLen32+2)
	buf = append(buf, '"')
	buf = base58.AppendEncode32(buf, (*[32]byte)(&p))
	buf = append(buf, '"')
	return buf, nil
}

func (p publicKey) String() string {
	return base58.EncodeCached32((*[32]byte)(&p))
}

var encodedBytes []byte
var encodedString string

func TestAppendEncode32_ValueReceiverAllocations(t *testing.T) {
	key := publicKey{1, 2, 3, 4, 5, 6, 7, 8}
	allocs := testing.AllocsPerRun(100, func() {
		encodedBytes, _ = key.MarshalJSON()
	})
	if allocs != 1 {
		t.Fatalf("MarshalJSON allocated %g times, want only the output allocation", allocs)
	}
	want := `"` + base58.Encode32((*[32]byte)(&key)) + `"`
	if string(encodedBytes) != want {
		t.Fatalf("got %q, want %q", encodedBytes, want)
	}
}

func TestEncodeCached32_ValueReceiverZeroAlloc(t *testing.T) {
	key := publicKey{1, 2, 3, 4, 5, 6, 7, 8}
	want := key.String() // Warm the cache before counting allocations.
	allocs := testing.AllocsPerRun(100, func() {
		encodedString = key.String()
	})
	if allocs != 0 {
		t.Fatalf("cached String allocated %g times, want zero", allocs)
	}
	if encodedString != want {
		t.Fatalf("got %q, want %q", encodedString, want)
	}
}

// Returning a slice into a local array must keep that array alive. Keeping
// this call out of line exercises the exported function's escape summary.
//
//go:noinline
func encodeLocalDestination(p publicKey) []byte {
	var storage [base58.EncodedMaxLen32 + 1]byte
	storage[0] = ':'
	return base58.AppendEncode32(storage[:1], (*[32]byte)(&p))
}

func TestAppendEncode32_ReturnedDestinationLifetime(t *testing.T) {
	var results [64][]byte
	var expected [64]string
	for i := range results {
		key := publicKey{byte(i)}
		expected[i] = ":" + base58.Encode32((*[32]byte)(&key))
		results[i] = encodeLocalDestination(key)
	}
	runtime.GC()
	for i, got := range results {
		if string(got) != expected[i] {
			t.Fatalf("result %d: got %q, want %q", i, got, expected[i])
		}
	}
}

func BenchmarkAppendEncode32_ValueReceiverJSON(b *testing.B) {
	key := publicKey{1, 2, 3, 4, 5, 6, 7, 8}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		encodedBytes, _ = key.MarshalJSON()
	}
}

func BenchmarkEncodeCached32_ValueReceiver(b *testing.B) {
	key := publicKey{1, 2, 3, 4, 5, 6, 7, 8}
	encodedString = key.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodedString = key.String()
	}
}
