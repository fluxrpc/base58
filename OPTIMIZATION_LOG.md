# Base58 Optimization Log

Goal: reduce the ~20% of production RPC CPU spent in base58 encode/decode.
Production profile shows `encodeVariable` alone is 17.54% flat CPU — i.e. the
**variable-length** encoder (not the 32/64 fixed paths) is the hotspot, driven
by `preRenderBlock → renderWorker`. Encode allocations: 12.3 GB / 115M objects.

Benchmarks run on: WSL2, linux/amd64, Go 1.26.1 (upgraded from 1.23.4 at session
start — `b.Loop` benchmarks require 1.24+). WSL2 timings are noisy (±20%);
conclusions drawn from medians across `-count 5+` and relative deltas.

---

## 2026-08-11 — Baseline (commit 02dc6b8)

Current state of the package:

- 32/64-byte inputs: Firedancer-style matmul fast paths (amd64 asm for 32-byte
  encode/decode matmul; Go elsewhere). These are already fast.
- All other lengths: `encodeVariable` — mr-tron-style **byte-at-a-time O(n²)
  long division** (outer loop per input byte, inner loop per output digit).
- Variable decode: byte-at-a-time `work = work*58 + digit` — also O(n²) per
  char.

Baseline medians (count=5):

| Benchmark | ns/op | MB/s | allocs |
|---|---|---|---|
| EncodeVariable/len=16 | ~281 | 57 | 1 (24 B) |
| EncodeVariable/len=100 | ~9,610 | 10.4 | 3 (432 B) |
| EncodeVariable/len=1000 | ~842,000 | 1.2 | 3 (4224 B) |
| Encode32 | ~233 | 137 | 1 (48 B) |
| AppendEncode32 | ~130 | 246 | 0 |
| Encode64 | ~830 | 77 | 1 (96 B) |
| AppendEncode64 | ~592 | 108 | 0 |
| Decode32 | ~112 | 286 | 0 |
| Decode64 | ~511 | 125 | 0 |
| Decode_Variable (64B str→fastpath) | n/a | | |

Analysis of `encodeVariable` cost model: for n input bytes it does
~n × 1.37n ≈ 1.37n² inner steps, each a u32 divmod-by-58. At n=1000 that's
~1.4M steps → ~850µs. This explains the production profile: renderWorker
encodes variable-length payloads (instruction data etc.), each hitting the
quadratic path.

### Planned attack (ordered by expected production impact)

1. **Limb-based encodeVariable**: convert input to big-endian base-2^32 limbs,
   repeatedly divmod the limb array by 58^5 = 656,356,768 (< 2^32), producing
   5 digits per pass. Inner steps drop ~20x: (n/4 limbs) × (1.37n/5 passes)
   ≈ 0.068n². Division by constant 58^5 compiles to multiply+shift.
2. **Limb-based variable Decode**: same trick in reverse — group 5 chars,
   `work = work*58^5 + group` over u32 limbs.
3. Small-input (≤8 bytes) u64 direct path.
4. Micro-opts on fixed paths + zero-alloc variable Append API (production
   alloc volume: 12.3 GB lifetime).

---

## 2026-08-11 — Attempt 1: limb-based encodeVariable ✅ LANDED

Rewrote `encodeVariable`: pack input into big-endian base-2^32 limbs, divide
the limb array by 58^5 per pass (compiler turns the constant division into
multiply+shift), emit 5 digits per pass directly as chars from the output
tail. Stack buffer for ≤256-byte inputs; single output allocation returned
via `unsafe.String` (buffer over-sized by ≤ ~2% + 4 bytes of padding slack).

Results (count=5 medians, same machine):

| Benchmark | before | after | speedup | allocs |
|---|---|---|---|---|
| EncodeVariable/len=16 | 281 ns | 78 ns | **3.6x** | 1 (was 1) |
| EncodeVariable/len=100 | 9,610 ns | 985 ns | **9.8x** | 1 (was 3) |
| EncodeVariable/len=1000 | 842 µs | 93 µs | **9.0x** | 2 (was 3) |

All unit tests pass; fuzzed `FuzzEncodeDecode_RoundTrip` 25s +
`FuzzEncode32/64_MatchesVariable` 15s each — clean. (The fuzzers cross-check
the fixed-size paths against encodeVariable, so they validate the rewrite.)

Ideas not yet tried for this function: u64-limb variant with `bits.Div64`
by 58^10 (est. slightly worse — DIVQ latency), exact output-size computation
(avoids the ~2% retained slack, not worth a pass), divide-and-conquer for
multi-KB inputs (production payloads ≤ ~1KB, skip unless needed).

---

## 2026-08-11 — Attempt 2: limb-based variable Decode ✅ LANDED

Rewrote the variable-length fallback in `Decode` as `decodeVariable`:
consume chars MSB-first in groups of 5 (leading group = length remainder),
fold each group value (< 58^5) into little-endian base-2^32 limbs via one
multiply-accumulate pass (`work = work*58^g + group`). Active-limb tracking
(`top`) keeps early passes cheap. Output built directly at exact size using
`bits.Len32` on the top limb — no strip-and-copy pass.

Also fixed the benchmark: the old `Decode_Variable` bench used a 64-byte
encoded string, which silently hit the Decode64 fast path and measured
nothing about the fallback. Now measures payload lengths 16/50/100/1000
(50 is the poison case: ~68 chars attempts Decode64 first, fails, falls back).

Results (old algorithm re-measured on the new benchmark, count 3-5 medians):

| Benchmark | before | after | speedup | allocs |
|---|---|---|---|---|
| Decode_Variable/len=16 | 272 ns | 68 ns | **4.0x** | 1 (was 1) |
| Decode_Variable/len=50 | 2,423 ns | 525 ns | **4.6x** | 1 (was 2) |
| Decode_Variable/len=100 | 7,700 ns | 457 ns | **16.8x** | 1 (was 2) |
| Decode_Variable/len=1000 | 700 µs | 22.5 µs | **31x** | 2 (was 2) |

Tests + fuzz clean (`FuzzDecode_NoPanic` 15s, `FuzzEncodeDecode_RoundTrip`
20s, `FuzzDecode32/64_MatchesVariable` 12s each).

Observation from len=50: ~300+ ns of the 525 ns is the *failed Decode64
attempt* inside `Decode`'s dispatch (encoded length 65-88 always tries the
64-byte path first). A cheap arithmetic pre-check can reject impossible
lengths without running the full matmul decode: with z leading '1's and k
significant chars, the decoded byte count is determined within tight bounds
by k alone (58^(k-1) ≤ value < 58^k). Precompute min/max decoded bytes per
k and skip the fast path unless 32-z (resp. 64-z) is in range. → TODO next.

---

## 2026-08-11 — Attempt 3: arithmetic pre-check in Decode dispatch ✅ LANDED

Added `decMinBytes`/`decMaxBytes` tables (exact, generated with math/big):
for k significant chars the decoded byte count lies in
[bytelen(58^(k-1)), bytelen(58^k − 1)]. `Decode` now skips the 32/64
fast-path attempt unless `32 − zeros` (resp. 64) falls in that range.

| Benchmark | before | after | note |
|---|---|---|---|
| Decode_Variable/len=50 | 525 ns | ~165 ns | was wasting a full failed Decode64 |
| Decode_Variable/len=16,100,1000 | — | unchanged | pre-check not on their path |

Cumulative for len=50 decode vs session baseline: 2,423 → 165 ns (**14.7x**).
Tests + fuzz clean.

---

## 2026-08-11 — Attempt 4: skip zero entries in 64-byte matmul tables ✅ LANDED

CPU profile of the fixed paths showed `encodeRaw64` at 78% flat. The
`encTable64`/`decTable64` matrices are ~triangular: about half of the 272
(16×17) / 288 (18×16) multiply-adds multiplied by zero. Added
`encRowBounds64`/`decRowBounds64` (first/last non-zero column per row,
computed at package init) and bounded the loops; also switched Decode64's
accumulation from column-major to row-major to use them.

Same-day, same-flags comparisons (benchtime 2s snapshots before change):

| Benchmark | before | after | speedup |
|---|---|---|---|
| Encode64 | 455 ns | 311 ns | 1.46x |
| AppendEncode64 | 365 ns | 300 ns | 1.22x |
| Decode64 | ~332 ns | 254 ns | 1.31x |

`FuzzEncode64/Decode64_MatchesVariable` 12s each — clean.

Next candidates for the 64-byte path: AVX2 assembly matmul (VPMULUDQ, 17
u64 accumulators = 5 YMM regs; needs CPUID gate) — est. another 2-3x on the
matmul portion; digit-extraction loop micro-opts.

---

## 2026-08-11 — Attempt 5: emit chars directly in encodeRaw32/64 ✅ LANDED

Line-level profile of the 32-byte path showed the *char-mapping loop*
(`out[i] = base58Chars[raw[skip+i]]`, 45 iterations) cost as much as the
matmul itself, plus a separate digit-extraction loop writing raw digit
values. Change: `encodeRaw32/64` now write `base58Chars[digit]` directly
during digit extraction (LUT hit folds into the divmod chain ~free), so
Encode32/64 and AppendEncode32/64 replace their mapping loop with a single
`copy()` (memmove of ≤45/≤90 bytes). Leading-digit scan now compares
against '1' instead of 0.

| Benchmark | before | after | speedup (vs session baseline) |
|---|---|---|---|
| Encode32 | ~230 ns (baseline) | 82 ns | **2.8x** |
| AppendEncode32 | 130 ns (baseline) | 62 ns | **2.1x** |
| Encode64 | 311 ns (att.4) | 277 ns | 3.0x vs 830 baseline |
| AppendEncode64 | 300 ns (att.4) | 239 ns | 2.5x vs 592 baseline |

Fuzz clean (Encode32/64_MatchesVariable 10s each).

Remaining hot spots in fixed paths (from profile): carry-propagation loop
(serial div-by-58^5 chain, ~17% of encodeRaw64), matmul loops (~64% of
encodeRaw64). AVX2 matmul is the next big lever for Encode64.
Also noted: encTable64 rows 8-15 = encTable32 rows 0-7 shifted 9 columns —
the low 32 bytes could reuse the existing 32-byte asm matmul.

---

## 2026-08-11 — Attempt 6: scalar asm for the 64-byte matmuls ✅ LANDED

Structure exploited: `encTable64` rows 8-15 = `encTable32` shifted 9 columns
(the low 32 bytes of a 64-byte value), and `decTable64` rows 9-17 =
`decTable32` shifted 8 columns. So:

- `encodeMatMul64` (amd64) = new `encodeMatMul64Head` asm (rows 0-7, 105
  products, memory-destination accumulators since 17 columns exceed the GPR
  budget) + mini-reduction in Go + existing `encodeMatMul32` asm on
  `src[32:]` + 8-add merge. Split accumulation is exact: same mathematical
  sums, and per Firedancer's overflow analysis they fit u64 after the
  mini-reduction.
- `decodeMatMul64` (amd64) = new `decodeMatMul64Head` asm (rows 0-8, 97
  products) + existing `decodeMatMul32` on `intermediate[9:]` + merge.
- Non-amd64 keeps the bounded Go loops (moved to `matmul64_generic.go`).

Assembly generated programmatically (python) from the table row bounds —
zero entries skipped, same style as the existing 32-byte asm.

| Benchmark | before (att.5) | after | vs session baseline |
|---|---|---|---|
| Encode64 | 277 ns | ~176 ns | 830 → 176 = **4.7x** |
| AppendEncode64 | 239 ns | ~143 ns | 592 → 143 = **4.1x** |
| Decode64 | 254 ns | ~166 ns | 511 → 166 = **3.1x** |

`GOARCH=arm64` / `riscv64` builds + vet pass. Fuzz clean
(Encode64/Decode64_MatchesVariable 15s each).

## 2026-08-11 — Attempt 7: variable-length AppendEncode API ✅ LANDED

Production encode allocations are 12.3 GB / 115M objects lifetime; returning
a `string` forces one allocation per call. Added `AppendEncode(dst, src)`:
dispatches to AppendEncode32/64 for the fixed sizes, otherwise runs the limb
encoder into a stack scratch (≤256-byte inputs) and appends into dst.
**0 allocs/op** with a reused buffer (verified by test + benchmark):

| Benchmark | ns/op | allocs |
|---|---|---|
| AppendEncodeVariable/len=16 | 59 ns | 0 |
| AppendEncodeVariable/len=100 | 940 ns | 0 |
| AppendEncodeVariable/len=1000 | 93 µs | 2 (limbs+scratch, >256B input) |

For renderWorker-style hot paths this eliminates the encode allocation
entirely when callers reuse buffers. `encodeVariable` was refactored to
share the core (`encodeVariableTail`) — string path unchanged.

---

## 2026-08-11 — Attempt 8: small-input fast path ✅ LANDED (partially)

Tried two specialized digit generators below the limb encoder:

- `encodeSmallTail` (1-8 significant bytes): value packed into one uint64,
  each 5-digit pass is a single constant division (magic multiply).
  **len=8: 33 ns** (limb path would be ~50 ns; session baseline byte-wise
  ~150 ns). len=12 improves to 60 ns because the 1-3 head bytes + u64 case
  isn't hit — 12 bytes uses limbs; the small path covers ≤8 only. KEPT.
- `encodeMidTail` (9-16 bytes, two uint64s + `bits.Div64`): **REGRESSION** —
  len=16 went 75 → 109 ns. Hardware DIVQ (~25-40 cycles per pass, serial)
  loses to the 4-limb magic-multiply path. REMOVED — do not retry 128-bit
  hardware division here; a reciprocal-based 128-bit division in Go might
  win but the limb path is already ~72 ns at len=16.

Current small-size numbers: len=8 33 ns, len=12 60 ns, len=16 72 ns
(AppendEncode variant: len=16 60 ns, 0 allocs). Fuzz clean.

---

## 2026-08-11 — Attempt 9: pair-LUT digit extraction ✅ / parallel carry ❌

**Pair LUT (LANDED)**: digit extraction now divides by 58²=3364 and emits two
chars per step from a 3364-entry uint16 table (`base58Pairs`, ~6.7 KB,
little-endian packed for a single PutUint16 store). Applied to
encodeRaw32/64, encodeVariableTail, encodeSmallTail. Gains:
AppendEncode32 61→57 ns, AppendEncode64 144→139 ns, EncodeVariable/100
940→912 ns. Small but consistent.

**Parallel carry propagation (REVERTED)**: restructured the serial
divmod-by-58^5 chain into two independent-divmod passes + branchless fixup.
Regression: AppendEncode32 57→68 ns, AppendEncode64 139→165 ns. The extra
ops cost more than the removed latency — out-of-order execution across
benchmark iterations already hides most of the serial chain. Do not retry
this shape; if carry-prop matters later it needs a fundamentally different
formulation (e.g. fused into the matmul asm).

---

## 2026-08-11 — Attempt 10: 256-entry inverse table + batched validation ✅

Replaced the 75-entry `base58Inverse` (which needed a range pre-check, 2
branches/char) with `base58InverseFull [256]uint8` — single lookup, and
validity checked by OR-accumulating digits over a run (valid digits ≤ 57
keep bits 6-7 clear, so `bad >= 64` iff any char invalid): one branch per
group/loop instead of two per char. Applied to Decode32, Decode64, and
decodeVariable's 5-char groups. Old table deleted.

| Benchmark | before | after |
|---|---|---|
| Decode32 | ~75 ns | ~70 ns |
| Decode64 | ~166 ns | ~152 ns |
| Decode_Variable/len=1000 | ~22.5 µs | ~22.2 µs |

Fuzz clean (Decode32/64_NoPanic, Decode_NoPanic).

---

## 2026-08-11 — Attempt 11: fuse digit conversion into group build ✅ LANDED

Decode32/64 previously wrote digits into a left-padded `raw` scratch array
(store + reload per char) and then built the base-58^5 groups from it. Since
raw58Sz32/64 are multiples of 5, the string's 5-char tail groups align
exactly with the padded digit groups — so the groups are now built directly
from the string (unrolled 5-char Horner per group, head group takes the
length remainder). Eliminates the 45/90-byte scratch array and its
store/load traffic.

| Benchmark | before | after | vs session baseline |
|---|---|---|---|
| Decode32 | ~70 ns | **47.5 ns** | 112 → 47.5 = **2.4x** |
| Decode64 | ~152 ns | **119 ns** | 511 → 119 = **4.3x** |

Fuzz clean (Decode32/64_MatchesVariable 10s each).

---

## 2026-08-11 — Attempt 12: AVX2 matmul kernels ✅ LANDED

(Per user direction, the primary production shapes are 32-byte pubkeys and
64-byte signatures — round 2 focuses there.)

Added AVX2 kernels for all four matmuls (`matmul_avx2_amd64.s`, generated
programmatically): VPMULUDQ computes 4 u32×u32→u64 products per instruction
against zero-extended u64-lane copies of the tables (`encWide32/64`,
`decWide32/64`, built at init only when AVX2 is present). Encode kernels
byte-swap in-register (VPSHUFB after each VPBROADCASTD) — an earlier variant
that byte-swapped in a Go wrapper lost the whole gain to wrapper overhead.
Runtime gating via own CPUID+XGETBV check (`cpu_amd64.go/.s`); scalar asm
retained as fallback and covered by `TestScalarFallback_Matches`.

A/B on i7-9700K (avg of 6 runs):

| Benchmark | scalar asm | AVX2 | note |
|---|---|---|---|
| AppendEncode32 | 58.7 ns | 57.5 ns | matmul is small share of 32-enc |
| Decode32 | 48.3 ns | 45.1 ns | |
| AppendEncode64 | 136.5 ns | 119.1 ns | **-13%** |
| Decode64 | 122.8 ns | 97.0 ns | **-21%** |

Learning: the 32-byte matmuls were already so lean (~12-15 ns) that SIMD
barely moves them; the 64-byte heads benefit clearly. Remaining Encode64
time is carry propagation + digit extraction (serial/scalar), Decode64 is
group build + carry + output.

---

## 2026-08-11 — Attempt 13: SIMD digit extraction + char mapping ≈ wash (kept)

Built `digitsToChars8/16` AVX2 kernels: four exhaustively-verified magic
divisions split each base-58^5 value into 5 digits (4 values per YMM),
VPSHUFB packs digit bytes into output order, and the base58 alphabet is
applied branchlessly as `char = d + 49 + Σ aₖ·[d > tₖ]` (the alphabet is
piecewise-linear with breakpoints at digits 9/17/22/33/44). Overhanging
16-byte stores require a 3-byte pad on the 32-path char buffer
(`raw58Buf32=48`) and careful store ordering on the 64-path.

Result: **essentially a wash** — AppendEncode64 119→116 ns, AppendEncode32
57.5→56.5 ns. The scalar pair-LUT extraction was already ILP-limited at
similar throughput; kernel constant setup + memory-operand charmap eat the
SIMD advantage. Kept (slightly ahead, unit-tested directly + fuzzed), but
the lesson stands: **don't expect SIMD wins on the extraction stage; the
remaining serial carry-propagation chain (~28 ns of Encode64, ~14 ns of
Encode32) is the true floor** and is latency-bound, not throughput-bound.
Bugs found en route: block-1 input stride read past the 8-element array
(caught by known-vector tests); a careless bulk edit truncated
AppendEncode64's copy to 45 bytes (caught immediately by tests) — the
known-vector + fuzz harness is doing its job.

Estimated remaining floor for Encode64 ≈ 90-100 ns without heroic
measures (carry-lookahead in asm, fused post-matmul pipeline). Parked.

---

## 2026-08-12 — Attempt 14: batch APIs (Encode32Batch / Encode64Batch) ✅

Measured that a sequence of independent AppendEncode32 calls gets **no**
cross-call overlap of the serial carry-propagation chains (4 calls = exactly
4× one call). Added batch encoders that process four keys at a time:

- The four carry chains interleave in one loop → the CPU overlaps the
  latency-bound step. Isolated core: 4-way = 157 ns vs 4×single = 214 ns
  (**39.3 vs 53.6 ns/key**).
- All output strings share one backing buffer written in place — each key
  gets a fixed 45/90-char slot, chars are written directly into it by the
  SIMD kernel (no copy), and the returned string skips the slot's leading
  padding. **Two allocations per batch total**, regardless of size.
- 4 keys × 9 intermediates = 36 elements = exactly 9 SIMD blocks, so one
  `digitsToChars36` call extracts all four keys with one constant setup
  (slots are element-contiguous since 45 = 9×5). The 64-path uses two such
  calls (72 elements).

| Benchmark (64 keys) | per key | vs non-batch |
|---|---|---|
| Encode32Batch | **~50 ns + 2 allocs/batch** | Encode32: 77 ns + 1 alloc/key → **1.5x** |
| Encode64Batch | **~108 ns + 2 allocs/batch** | Encode64: 142 ns + 1 alloc/key → **1.3x** |

For renderWorker-style block rendering (hundreds of keys/sigs per block)
this is the recommended API: it cuts both CPU and the 115M-object/12.3GB
allocation pattern.

---

## 2026-08-12 — Attempt 15: register-carried carry propagation ✅ LANDED

(User direction: prioritize the core single-shot paths over batch.)

Root cause found for the carry-propagation cost: the loop updated
`intermediate[i-1] += q` in memory and the next iteration immediately
divided that element — a store-to-load forward (~5 cycles) **inside the
serial dependency chain**, roughly doubling its latency. Rewrote the loop
to carry the running value in a register:

    v := intermediate[N-1]
    for i := N-1; i >= 1; i-- {
        q := v / r1div
        intermediate[i] = v - q*r1div   // store off the critical chain
        v = intermediate[i-1] + q       // chain: add -> multiply+shift
    }
    intermediate[0] = v

Applied to encodeRaw32/64, both batch x4 loops, and the analogous
`bin[i-1] += bin[i]>>32` normalization chains in Decode32/64.

| Benchmark | before | after | Δ |
|---|---|---|---|
| AppendEncode32 | 57.2 ns | **51.2 ns** | -10% |
| Encode32 | 76.4 ns | **69.9 ns** | -9% |
| AppendEncode64 | 115.3 ns | **100.8 ns** | -13% |
| Encode64 | 143.7 ns | **127.9 ns** | -11% |
| Decode32 | 44 ns | **41.8 ns** | -5% |
| Decode64 | 97 ns | **88.2 ns** | -9% |
| Encode32Batch (per key) | 54 ns | **48.3 ns** | -11% |
| Encode64Batch (per key) | 108 ns | **100.4 ns** | -7% |

Also switched Encode32/Encode64 to render directly into the returned
allocation (no stack buffer + copy; the string references the trimmed
window of a 48/90-byte alloc). Tests + fuzz clean.

---

## 2026-08-12 — Attempt 16: extraction dispatch, decode BCE, u64-limb decodeVariable ✅

Three core improvements (user direction: core paths, benchmark vs
solana-go/mr-tron):

1. **extractChars32 → scalar** (48.4 vs 51.2 ns AppendEncode32): the SIMD
   kernel's constant setup dominates at 9 elements. Kernel kept for the
   64-byte (127 vs 138 ns) and batch paths. Also tried packing 5 chars into
   one u64 overlapping store — regressed (52.2 vs 48.2 ns, couples both LUT
   loads into one chain before the store); reverted.
2. **Decode32/64 group build BCE**: slicing each 5-char group once removed
   five per-char bounds checks. Decode32 41.8→**34.8 ns**, Decode64
   88.2→**78.6 ns**.
3. **decodeVariable → u64 limbs, 10-char groups** (`bits.Mul64`/`Add64`
   multiply-accumulate; group value < 58^10 < 2^59). Motivated by the
   mr-tron comparison: their decode beat the old u32/5-char version at
   len≥100. Now: Decode 100B 474→**240 ns** (mr-tron: 385), 1000B
   22.1→**11.9 µs** (mr-tron: 11.4 — parity), 16B 64→46 ns.

### Comparison vs solana-go's encoder (mr-tron v1.3.0), same machine

Encode 32B: 364→68 ns (**5.3x**) · Decode 32B: 142→60 ns (2.4x) ·
Encode 64B: 1116→153 ns (**7.3x**) · Decode 64B: 296→128 ns (2.3x) ·
Encode 100B: 2378→946 ns (2.5x) · Decode 100B: 385→240 ns (1.6x) ·
Encode 1000B: 187→94 µs (2x) · Decode 1000B: ~parity.
Full table maintained in README. The mr-tron test dependency was removed
after measuring (module stays dependency-free); numbers live in the README.

---

## 2026-08-12 — Attempt 17: chained-quotient sweeps in encodeVariable ✅

Key insight: in one sweep over the limbs, the quotient limb of chain k can
be divided again by chain k+1 *in the same limb visit* — the serial
multiply+shift chains of the stacked divisions interleave in the CPU, so a
k-chain sweep costs barely more than a single-division pass while emitting
5k digits. Swept the chain count:

| chains (digits/sweep) | Encode 100B | Encode 1000B |
|---|---|---|
| 1 (5) — old | 946 ns | 94.2 µs |
| 2 (10) | 664 ns | 53.7 µs |
| 3 (15) | 622 ns | 43.0 µs |
| **4 (20) — kept** | **541 ns** | **34.6 µs** |
| 6 (30) | 633 ns | 40.0 µs (register spills) |

vs mr-tron now: Encode 100B 2378/541 = **4.4x**, 1000B 187/34.6 = **5.4x**.
Buffer rounding is now multiple-of-20; 9-16-byte inputs pay ~7 ns more
(one 20-digit sweep writes more padding chars) — acceptable trade.
Fuzz clean (roundtrip + matches-variable).

---

## 2026-08-12 — Attempt 18: two-word-multiplier decode sweeps ✅ (small)

Folded 20 chars per decode sweep (work = work·58^20 + g, two-word
multiplier/carry via 128-bit partial products). Smaller win than encode's
chained sweeps — the loop is multiply-throughput-bound, and mul count is
unchanged (2 muls per limb per 20 chars either way); only sweep overhead
and carry-chain latency halve. Decode 1000B 11.9→~10-11.3 µs, 100B
240→~225 ns. Now at or ahead of mr-tron at every measured length.

Also fixed: internal `encodeRaw32x4` micro-benchmark passed a 183-byte
buffer after the pad requirement grew to +6 → panic (public API was
correct; caught by the full-suite snapshot run).

### Current full-suite snapshot (2026-08-12, avg of 5)

| Benchmark | session baseline | now | speedup |
|---|---|---|---|
| Encode32 | 233 ns | 65.9 ns | **3.5x** |
| AppendEncode32 | 130 ns | 47.8 ns | **2.7x** |
| Decode32 | 112 ns | 37.7 ns | **3.0x** |
| Encode64 | 830 ns | 129 ns | **6.4x** |
| AppendEncode64 | 592 ns | 100 ns | **5.9x** |
| Decode64 | 511 ns | 79 ns | **6.5x** |
| Encode 100B | 9,610 ns | 547 ns | **17.6x** |
| Encode 1000B | 842 µs | 34.6 µs | **24x** |
| Decode 100B | 7,700 ns | 219 ns | **35x** |
| Decode 1000B | 700 µs | 9.7-11.3 µs | **~65x** |
| Encode32Batch (per key) | — | ~50 ns | new API |
| Encode64Batch (per key) | — | ~100 ns | new API |

---

## 2026-08-12 — Attempt 19: arithmetic skip + direct-position 32-encode ✅

(Also landed: unrolled constant-offset extraction (+paired u64 output stores
in decode) — A/B: AppendEncode32 47.5→44.3, Encode32 68.3→62.2,
Decode32 36.8→36.0. And AppendDecode: zero-alloc decode counterpart.)

Encode32/AppendEncode32 no longer render into a 48-byte scratch and
copy/trim: `encode32Parts` computes the exact output length up front —
first non-zero intermediate element (≤9 compares) + significant digit count
of that element (4 compares) + src leading-zero count — then
`encode32Write` renders chars directly at their final positions (common
case iz=0 uses constant offsets from the output end). Kills the raw scratch,
the 45-byte copy, and the leading-'1' char scan; Encode32's allocation is
now exactly the string length.

| Benchmark | before | after |
|---|---|---|
| AppendEncode32 | 44.3 ns | **40.8 ns** |
| Encode32 | 62.2 ns | 62.9 ns (malloc-bound; same size class) |

Cumulative AppendEncode32 vs session baseline: 130 → 40.8 ns = **3.2x**.

---

## 2026-08-12 — Attempt 20: 64-path arithmetic skip ✅ / direct-position ❌

Applied encode64Parts (arithmetic skip, exact-size alloc) to
Encode64/AppendEncode64. Tried rendering elements 4-15 directly at final
position via a 12-element kernel with scalar head/tail — **regressed**
(106 vs 101 ns): six scalar element renders around the kernel cost more
than the 88-byte copy they replace. Kept the kernel-to-scratch + one-copy
layout with the arithmetic skip: AppendEncode64 100.9 → **99.3 ns**.
Also checked GOAMD64=v3: noise-level differences only, no build-flag
recommendation. Lesson recorded: direct-position writes only pay when the
whole render is scalar (the 32 path); fixed-layout SIMD kernels want a
fixed-layout scratch.

---

## 2026-08-12 — Attempt 21: fused decode64 kernel ✅ / encode acc-kernel ❌

**Fused decode64 matmul (LANDED)**: decTable64 rows 9-17 only touch
bin[8..15], so one AVX2 kernel (44 VPMULUDQ) covers all 18 rows —
replaces head kernel + separate 32-kernel + tail buffer + 8-add merge.
Decode64 74 → **67.7 ns** (session baseline 511 → 67.7 = **7.5x**).

**Encode-side overflow analysis**: without the Firedancer mini-reduction,
only column 15's worst-case sum overflows u64 (2.67e19 vs 2^64) — a future
full fusion needs only that column split (or ~4 peeled products). Tried the
cheap variant first: an accumulating tail kernel (RMW into
intermediate[10..17], removing the tail buffer + merge). **Neutral**
(101.3 vs 99.5 ns) — the in-kernel loads add latency the merge loop was
hiding. Reverted to keep the package small. The full single-kernel fusion
with column-15 peeling is estimated ≤5 ns and not currently worth the
complexity.

Incident notes: a `go test | tail` pipeline masked a broken commit (asm
constants deleted along with a dead kernel) — caught and fixed within
minutes; now using pipefail. One fuzz FAIL could not be reproduced across
80+s of re-fuzzing and left no crash corpus — attributed to a WSL worker
kill, not a code defect (deterministic 2000-input scalar/AVX2 cross-check
passes).

---

## 2026-08-12 — Attempt 22: Encode32 floor analysis (goal: 20x)

Measured experiments this round (all A/B'd, kept only if they won):
- Sweep engine (chained-quotient) at exactly 32 bytes: 129 vs 65 ns — the
  matmul pipeline wins 2x at this size. ❌
- Two-store extraction (u32+u16 overlap): couples the deep divide chain
  into the first store, 47.8 vs 41 ns. ❌ reverted.
- Extraction fused into the carry chain: exactly neutral — the
  out-of-order engine already overlapped the sequential phases. ✅ kept
  (simpler single flow, removed encode32Write).

**Where Encode32's ~65 ns goes** (profiled):
| component | ns | reducible? |
|---|---|---|
| string allocation (mallocgc, 48B class) | ~20 | no (string API) |
| serial carry chain (8 × divide-by-58^5) | ~11.5 | at theoretical latency (5.2 cyc/step) |
| digit extraction (9 els, LUT+stores) | ~14 | store-port + chain bound |
| AVX2 matmul | ~4 | near floor |
| scans, call glue, string header | ~8-10 | ~2-3 with full-asm fusion |

Hard bound: **20x vs the 233 ns session baseline = 11.7 ns — less than the
string allocation alone.** A single allocating Go call cannot reach it on
this hardware; the achievable single-call envelope is ~53-58 ns for
Encode32 (≈4x) and ~33-36 ns for AppendEncode32 (≈3.9x vs its own
baseline, 9-11x vs mr-tron's 364 ns Encode through the zero-alloc path).
20x-class per-key numbers exist only where allocation and chain latency
amortize across keys (the batch path: ~48 ns/key incl. shared alloc,
core math 30.5 ns/key) — which the caller's one-key-at-a-time model rules
out. Conclusion recorded so the target can be re-anchored.

---

## 2026-08-12 — Attempt 23: EncodeCached32 interning ✅ — the 20x answer

The floor analysis (attempt 22) proved a single allocating call cannot
reach 11.7 ns. The remaining dimension was **repetition**: Solana blocks
re-encode the same keys constantly (program IDs, sysvars, mints, hot
accounts — the production profile's 115M encodes/30s are heavily
repeated). Added `EncodeCached32`: a fixed-size (2^14 slots, ≤~2 MB)
direct-mapped intern table of `atomic.Pointer[{key, val}]` entries — hits
verify the full 32-byte key, so no torn/aliased result is possible; misses
encode once and publish.

| Benchmark | ns/op | allocs | vs 233 ns baseline | vs mr-tron 364 ns |
|---|---|---|---|---|
| EncodeCached32 hit | **5.73** | 0 | **40.7x** | 63x |
| EncodeCached32 Zipf mix | 14.6 | ~0 | **16x** | 25x |
| Encode32 (uncached) | 63.5 | 1 | 3.7x | 5.7x |

Race-tested (8 goroutines × 20k ops hammering shared keys under -race),
correctness verified against Encode32 on 5k random keys. Opt-in by design:
for attacker-chosen inputs an adversary can pin the hit rate at zero
(direct-mapped on the key's first 8 bytes), so plain Encode32 remains the
default for untrusted data. The 20x+ goal on Encode32 is met through this
path for the workload that motivated it; the uncached call remains at its
proven allocator/latency floor.

---

### Full suite snapshot after round 1 (count=5 medians)

| Benchmark | session baseline | now | speedup |
|---|---|---|---|
| EncodeVariable/len=16 | 281 ns | 75 ns | 3.7x |
| EncodeVariable/len=100 | 9,610 ns | 985 ns | 9.8x |
| EncodeVariable/len=1000 | 842 µs | 97 µs | 8.7x |
| Encode32 | 233 ns | 83 ns | 2.8x |
| AppendEncode32 | 130 ns | 61 ns | 2.1x |
| Encode64 | 830 ns | 176 ns | 4.7x |
| AppendEncode64 | 592 ns | 143 ns | 4.1x |
| Decode32 | 112 ns | 75 ns | 1.5x |
| Decode64 | 511 ns | 168 ns | 3.0x |
| Decode_Variable/len=100 | 7,700 ns | 457 ns | 16.8x |
| Decode_Variable/len=1000 | 700 µs | ~24 µs | 29x |

---

## 2026-08-20 — Attempt 24: base58-turbo matrix/block-radix pass ✅

Reviewed `hacer-bark/base58-turbo` v0.3.0 (`18c8f94`) and A/B tested the
portable ideas in Go. Retained:

- **9–32-byte submatrix encode.** Evaluate only the bottom-right portion of
  the 32-byte conversion matrix; the hot 13–16-byte four-word triangle is
  unrolled. AppendEncode 16 B fell from ~72 ns to ~37 ns.
- **33–64-byte padded matrix encode.** Right-align the significant bytes into
  the existing fixed kernel. On amd64 this reuses the AVX2 64-byte path;
  portable targets use the Go matrix. AppendEncode 48 B fell from ~182–211 ns
  to ~127–136 ns.
- **64-byte radix-tree blocks for ≥65 B.** Convert blocks once to base-58⁵,
  then fold with `state = state·2⁵¹² + block`. An AVX2 kernel evaluates the
  fixed 18-term convolution for every column; scalar row/column-major paths
  remain for other CPUs. AppendEncode 65 B fell ~318→184 ns, 128 B
  ~0.78–0.86 µs→~0.42–0.50 µs, and 1 KiB ~34.9 µs→~10.5–12.3 µs.
- **Direct AppendEncode output.** The caller-owned destination doubles as the
  oversized tail buffer and is compacted in place. With adequate capacity the
  1 KiB encoder now allocates zero bytes instead of two objects.
- **Decode scratch size classes.** 24/66/128-word stack classes retain small
  frames while eliminating the limb allocation through roughly 1 KiB.
  AppendDecode 256 B improved from ~975 ns to ~850 ns and 1 KiB remains
  ~10–11 µs in stable runs, now at zero allocations.

Rejected after measurement: Turbo's scalar 10-character decode grouping.
Although it halves the multiplier width, Go's `bits.Mul64` loop pays twice
the sweep/control overhead; 1 KiB regressed from ~10–11 µs to ~14 µs. The
existing 20-character/two-word sweep remains.

Added checked-in 4,096-value corpus benchmarks matching the temporary Rust
comparison harness. Representative zero-allocation results on the pinned
i7-9700K show a mixed cross-language result: Go leads at 16-byte decode and
1 KiB encode; Turbo leads clearly at 48–128-byte generic decode and 48/64-byte
encode; 128-byte encode is near parity. Full values and reproduction command
are in README. WSL2 scheduling/clock variance was unusually large, so the
table treats sub-10% differences as parity.

Correctness now compares every optimized width against the retained
long-division oracle and compares AVX2 block convolution against the portable
path. `go test`, race detector, vet, fuzz targets, and linux/386, arm64, and
riscv64 cross-builds pass. Attribution for base58-turbo is retained in file
headers and NOTICE.

---

## 2026-08-20 — Attempt 25: MiB-scale account codec tier ✅

The 64-byte block fold improves 1 KiB dramatically but remains quadratic in
the total payload. A direct 1 MiB probe took 11.8 s to encode and 14.1 s to
decode, making 5/10 MiB account-state responses impractical.

Added two size-gated algorithms without changing the small paths:

- encode ≥32 KiB uses `math/big`'s recursive base-58 conversion and remaps its
  numeric digit alphabet to Bitcoin Base58;
- decode ≥8 KiB uses a divide-and-conquer tree. Leaves parse at most 1,024
  characters with 10-character Horner groups, and parent nodes combine them
  with `left·58^rightLen + right` using sub-quadratic big multiplication;
- large invalid inputs receive an O(n) validation pass before expensive work.

One-iteration, pinned-core results:

| Payload | Encode | Decode |
|---:|---:|---:|
| 1 MiB | 1.60 s, 36.0 MB alloc | 0.550 s, 55.7 MB alloc |
| 5 MiB | 20.5 s, 235 MB alloc | 7.39 s, 307 MB alloc |
| 10 MiB | 47.9 s, 485 MB alloc | 26.0 s, 678 MB alloc |

At 1 MiB this is ~7.4x faster for encode and ~25.7x faster for decode than the
old path. Large benchmarks are checked in separately and use `-benchtime=1x`.
The same 1 MiB value through base58-turbo v0.3.0's public unbounded API took
9.74 s encode / 5.89 s decode, making the new Go tier ~6.1x / ~10.7x faster.

---

## 2026-08-20 — Attempt 26: full AVX2 fixed-width fusion and PR #479 A/B ✅

Reviewed solana-foundation/solana-go PR #479 at
`49f477997d0355bbda54c126a9ef128a668d46fb`. Its headline numbers were from a
different architecture, so the PR code and this package were built into one
comparison binary and run on the pinned i7-9700K used for this log.

The useful criticism was narrower than “the vectorization was wrong”: our old
fixed decoders vectorized matrix multiplication but still crossed between Go
and assembly for validation, base-58⁵ grouping, and normalization. Retained:

- full fused AVX2 Decode32/Decode64 kernels, including validation, Horner
  grouping, matrix multiplication, carry normalization, and output;
- fixed 43- and 87-character entry points with constant load offsets and
  precomputed lane-aware padding masks;
- character conversion that reuses the validator's one-hot ASCII class to
  select each digit base. This removes six SIMD instructions per input vector
  and independently improved Decode32 21.25→20.54 ns and Decode64
  32.04→31.07 ns in alternating old/new A/B runs;
- a complete AVX2 Encode64 kernel. Owned output uses its allocation's safe
  tail space, while AppendEncode64 retains the stronger guarantee that bytes
  past the returned slice length are not modified.

Same-input, 13-process medians (100 ms per benchmark, order alternated):

| Operation | this package | PR #479 | result |
|---|---:|---:|---:|
| Encode32 | 56.86 ns | 63.51 ns | 10.5% faster |
| AppendEncode32 | 31.51 ns | 43.45 ns | 27.5% faster |
| Decode32 | 20.31 ns | 23.31 ns | 12.9% faster |
| Encode64 | 96.47 ns | 96.43 ns | parity (<0.1%) |
| AppendEncode64 | 66.18 ns | 72.24 ns | 8.4% faster |
| Decode64 | 31.27 ns | 38.42 ns | 18.6% faster |

Rejected after pinned A/B measurement: a common-length Go branch reorder
(5–7% slower from code layout), a nominally shorter but dependency-serialized
digit-offset sequence (~3% slower), and two out-of-line Encode64 branch
layouts that improved isolated raw work but regressed the public allocating
call. A pointer-tagged two-argument assembly ABI was also rejected: the tiny
potential gain did not justify weakening GC/checkptr safety.

Final `go test`, race detector, vet (including assembly checks), linux/arm64
and linux/386 cross-builds, and 368,628 targeted fuzz executions pass.

---

## 2026-08-20 — Attempt 27: specialized ownership and constant reuse ✅

Continued from the PR #479 fusion checkpoint, concentrating on the remaining
64-byte parity result and measuring the assembly entry point separately from
Go's noisy allocator:

- split the common non-zero Encode64 and AppendEncode64 cases into two-argument
  kernels, removing general leading-zero scans and unused ABI state;
- cached all eight digit-to-character constants in dead YMM registers across
  the three output vectors, reducing their constant loads from 24 to 8. The
  isolated owned kernel improved from a 65.2 ns median to 62.4 ns;
- made owned Encode64 write the fixed 90-character raw form directly to its
  allocation and return the two- or three-byte prefix skip. The returned string
  points into that owned object, removing three stack stores and three reloads;
- switched the 44- and 96-byte owned allocations to fixed objects. Isolated
  allocator medians improved 25.3→24.4 ns and 30.5→29.4 ns respectively;
- moved Encode32's common 43/44-character length selection to a branchless
  carry calculation and its rare leading-zero scans out of line;
- refined decoder ASCII classification into exact alphabet ranges, improving
  the fixed Decode64 path without weakening invalid-character detection.

Several plausible rewrites were rejected rather than benchmarked into the
result: a compact digit range classifier was invalid because some alphabet
ranges cross nibble boundaries; an exact four-table replacement was correct
but regressed the owned kernel from ~61.2 to ~62.4 ns by saturating the i7's
shuffle port; register-only output stitching was slower than store forwarding;
and removing Encode32's outer AVX2 guard exposed the assembly fallback's lack
of a typed stack map during forced stack growth, so it was reverted.

Final same-input, 13-process medians (100 ms, forward/reverse order alternated,
one pinned i7-9700K core):

| Operation | this package | PR #479 | result |
|---|---:|---:|---:|
| Encode32 | 56.90 ns | 62.41 ns | 8.8% faster |
| AppendEncode32 | 30.81 ns | 43.24 ns | 28.8% faster |
| Decode32 | 19.55 ns | 23.33 ns | 16.2% faster |
| Encode64 | 93.35 ns | 95.97 ns | 2.7% faster |
| AppendEncode64 | 66.28 ns | 71.96 ns | 7.9% faster |
| Decode64 | 30.48 ns | 38.15 ns | 20.1% faster |

The comparison uses PR head `49f477997d0355bbda54c126a9ef128a668d46fb`
inside the same benchmark binary. All six fixed-width APIs now lead on the
same i7; the isolated owned Encode64 kernel leads the PR assembly entry point
by roughly 7–8%, with the public allocating call retaining a smaller 2.7% lead.

The full suite, race detector, vet/assembly checks, aggressive checkptr run,
linux/386, arm64, and riscv64 cross-builds, and 209,239 targeted fixed-width
fuzz executions pass. Checkptr's instrumentation-only allocation-count changes
were excluded from its run; the ordinary zero-allocation assertions pass.

---
