// Package base58 implements fast base58 encoding and decoding (Bitcoin
// alphabet), tuned for Solana-shaped workloads.
//
// 32-byte and 64-byte inputs — public keys, hashes, signatures — dispatch to
// Firedancer-style matrix-multiply fast paths (AVX2 assembly on amd64,
// gated by runtime CPU detection, with scalar-assembly and pure-Go
// fallbacks). All other lengths use a limb-based codec that processes many
// digits per pass instead of byte-at-a-time long division.
//
// For hot paths, prefer the typed and appending APIs:
//
//   - Decode32/Decode64 decode into caller arrays with zero allocations.
//   - AppendEncode/AppendEncode32/AppendEncode64 and AppendDecode write into
//     a reused buffer with zero allocations.
//   - Encode32Batch/Encode64Batch encode slices of keys four at a time with
//     interleaved carry chains and two allocations per batch.
//
// The generic Encode/Decode remain drop-in replacements for other base58
// packages and are bit-compatible with Bitcoin Core, bs58, and mr-tron.
package base58
