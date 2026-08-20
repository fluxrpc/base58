# Optimizing Base58 for Solana-Shaped Workloads

*cloakd/base58 — engineering summary, August 2026*

## Abstract

A production RPC service spent 20% of its CPU (0.55 cores continuously) in
base58 encoding/decoding, and encode allocations totaled 12.3 GB / 115M
objects over the process lifetime. Starting from a codebase with
Firedancer-style fixed-size fast paths and a byte-at-a-time variable-length
codec, we rebuilt both codecs and the hot fixed paths, then added matrix and
64-byte block-radix dispatch for arbitrary-width encoding. Results on an
i7-9700K (AVX2), single core, Go 1.26:

| Path | baseline | final | speedup |
|---|---|---|---|
| Encode32 (pubkey) | 233 ns | 65 ns | **3.6x** |
| AppendEncode32 | 130 ns | 41 ns, 0 allocs | **3.2x** |
| Decode32 | 112 ns | 35 ns, 0 allocs | **3.2x** |
| Encode64 (signature) | 830 ns | 128 ns | **6.5x** |
| AppendEncode64 | 592 ns | 99 ns, 0 allocs | **6.0x** |
| Decode64 | 511 ns | 68 ns, 0 allocs | **7.5x** |
| Encode 100 B | 9.6 µs | ~0.4–0.6 µs | **~16–24x** |
| Encode 1000 B | 842 µs | ~11.7 µs | **~72x** |
| Decode 100 B | 7.7 µs | ~230 ns | **33x** |
| Decode 1000 B | 700 µs | ~10.5 µs | **~65x** |

Against `mr-tron/base58` (the encoder inside solana-go), the generic
`Encode`/`Decode` entry points are 5.3x / 2.4x faster at 32 bytes and
7.3x / 2.3x at 64 bytes, and now lead at every measured length.
Against `hacer-bark/base58-turbo` v0.3.0 using a shared 4,096-value corpus,
the zero-allocation Go path is competitive rather than universally faster:
it leads representative 16-byte decode and 1 KiB encode measurements, while
Turbo's generic SIMD decoder clearly leads at 48–128 bytes. See the README
for the current table and reproduction protocol.

## 1. Variable-length codec: radix widening

The baseline processed one **byte** (encode) or one **character** (decode)
per big-integer pass — O(n²) with a tiny constant per step, 1.37n² inner
steps for encode.

**Encode.** The input is packed into base-2³² limbs. Each sweep divides the
whole limb array by 58⁵ (a constant division the compiler lowers to
multiply+shift), emitting five digits. We then stack **four chained
divisions per limb visit**: chain *k+1* divides chain *k*'s quotient inside
the same sweep, so their serial multiply chains interleave in the CPU and a
sweep emits 20 digits for barely more than the cost of five. Six chains
regress (register spills); four is the knee. Digits are emitted directly as
output characters — two per constant division via a 58²-entry pair table.
Inputs of ≤8 bytes short-circuit through a single-uint64 path.

The retained final dispatcher goes further: 9–32 significant bytes evaluate
the bottom-right portion of the existing 32-byte matrix, 33–64 bytes are
right-aligned into the fixed-width matrix kernels, and ≥65-byte values are
folded 64 bytes at a time in radix 2⁵¹². Each block is converted to base 58⁵
once, then combined with a short 18-limb convolution; amd64 evaluates every
convolution column with AVX2. This reduces the 1 KiB append encoder from
~34.9 µs and two allocations to ~10.5–12.3 µs with zero allocations.

For account-sized payloads, continuing that Horner fold is still quadratic.
At 32 KiB encode therefore switches to `math/big`'s recursive base conversion;
at 8 KiB decode switches to a divide-and-conquer tree of bounded Horner leaves.
On one pinned i7-9700K core, encode/decode take 1.60/0.550 s at 1 MiB,
20.5/7.39 s at 5 MiB, and 47.9/26.0 s at 10 MiB. These are substantial wins
over the former path (already 11.8/14.1 s at 1 MiB), though base64 remains the
appropriate RPC encoding whenever the client permits it.

**Decode.** Characters fold most-significant-first in groups of 20 into
base-2⁶⁴ limbs: `work = work·58²⁰ + group`, using 128-bit
multiply-accumulate (`bits.Mul64`/`Add64`) with a two-word multiplier and
carry. Char validation is one branch per group: digits are ≤57, so an OR
accumulator ≥64 flags any invalid byte.

**Dispatch.** Exact tables of the min/max decoded byte count for *k*
significant characters (generated with `math/big`) reject impossible 32/64
fast-path attempts before paying for a failed matmul decode.

## 2. Fixed 32/64-byte paths

The Firedancer matmul structure was kept — measured against alternatives
(a chained-sweep engine at 32 bytes is 2x slower) it remains the right
decomposition — and then each surrounding stage was rebuilt:

- **Register-carried carry propagation.** The reference loop updates the
  array in place, which puts a ~5-cycle store-to-load forward *inside* the
  serial divide chain. Carrying the running value in a register halves the
  chain latency: −9…13% across all six fixed paths. This was the single
  largest micro-win of the project.
- **Direct character emission.** Digit extraction writes final base58
  characters (pair-LUT, two per division) instead of raw digits that a
  second loop remaps.
- **64-byte matmuls in assembly** by exploiting table structure: the 64-byte
  tables embed the 32-byte tables shifted (encode rows 8-15, decode rows
  9-17), so the existing 32-byte kernels serve as the tail halves.
- **AVX2 kernels, honestly gated.** VPMULUDQ matmul kernels (4 products per
  instruction, tables widened to u64 lanes at init, byte-swap in-register
  via VPSHUFB) and a digit-extraction/character-mapping kernel built on
  exhaustively-verified magic divisions and the alphabet's piecewise-linear
  form (`char = d + 49 + Σ aₖ·[d > tₖ]`). Each kernel was A/B measured and
  kept only where it wins: the 64-byte matmuls (decode fused into a single
  44-multiply kernel), and 64-byte/batch extraction. The 32-byte extraction
  stays scalar — kernel constant setup outweighs SIMD at 9 elements.
  Runtime CPUID gating; scalar assembly and pure-Go fallbacks are
  cross-checked by a dedicated test.
- **Arithmetic output sizing.** The number of leading pad characters is
  computed from the intermediates (first non-zero element + four compares)
  before anything is written, so encodes render straight into exact-size
  output with no trim copy or character scan, and decode group builds read
  the string directly with one bounds check per 5-character group.

## 3. Allocation surface

`Decode32/64` and all `Append*` functions are zero-allocation through roughly
1 KiB (account-sized inputs use the allocated divide-and-conquer tier);
`Encode/Encode32/Encode64` allocate exactly the returned string.
`AppendEncode` and `AppendDecode` cover the standard encode/decode call
pattern with reused buffers, eliminating the 12.3 GB/115M-object production
pattern where adopted. (Batch APIs with interleaved carry chains exist —
~48 ns/key — but single-call is the primary usage model.)

## 4. Negative results (measured, reverted, logged)

Parallel two-pass carry propagation (+20%: op count beats latency savings);
`bits.Div64` mid-size encode path (hardware divide loses to magic
multiplies); packed single-store character emission (couples both LUT loads
into one dependency chain); Go-side byte-swap wrappers around SIMD kernels
(wrapper erases kernel gain); six-chain sweeps (register spills);
memory-accumulating tail kernel and direct-position 64-byte kernel writes
(neutral or worse); GOAMD64=v3 (noise). Full details with numbers in
`OPTIMIZATION_LOG.md`.

## 5. Where the floor is — and stepping past it

Single-call `Encode32` at ~65 ns decomposes into: string allocation ~20 ns,
serial carry chain ~11.5 ns (at theoretical 5.2 cycles/step), digit
extraction ~14 ns (store-port/load-latency bound), matmul ~4 ns, glue ~8 ns.
Further meaningful reduction requires either dropping the allocation
(`AppendEncode32`, 41 ns) or amortizing across calls; the arithmetic itself
is within ~2x of its dependency-chain lower bound on this hardware.

The amortization that fits a one-key-at-a-time call model is
**interning**: Solana workloads re-encode the same keys constantly, so
`EncodeCached32` backs `Encode32` with a fixed-size (≤~2 MB) lock-free
direct-mapped intern table whose entries are verified against the full
32-byte key before use. Hits return in **5.7 ns with zero allocations**
(40x over baseline); a realistic Zipf key mix averages ~15 ns. It is
opt-in: for attacker-chosen inputs a collision-flooded cache degrades to a
plain encode plus an entry store, so untrusted data should use `Encode32`.

## 6. Verification

Bit-compatibility vectors against Bitcoin Core/bs58/mr-tron/five8; ~200M+
fuzz executions across eight targets cross-checking the fixed paths against
the independent variable-length implementation and AVX2 against scalar;
2000-input deterministic scalar/SIMD cross-test; race detector;
arm64/riscv64/386 builds. Every optimization attempt was committed
separately with its measurement.
