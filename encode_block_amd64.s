//go:build amd64

// Block-radix performance work informed by base58-turbo.
// Copyright 2026 AlphaBatem Labs.
// SPDX-License-Identifier: MIT OR Apache-2.0

#include "textflag.h"

// func encodeConvolveP512AVX2(padded *uint32, columns int, tmp *uint64)
//
// Fills every output column. padded points at 17 zero u32 limbs immediately
// before digit 0, and the caller clears the 17 limbs after the live input.
// Therefore every column is the same fixed 18-term dot product, including the
// triangular leading and trailing edges.
TEXT ·encodeConvolveP512AVX2(SB), NOSPLIT, $0-24
	MOVQ	padded+0(FP), SI
	MOVQ	columns+8(FP), AX
	MOVQ	tmp+16(FP), DI
	JLE	done
	LEAQ	·blockP512Wide(SB), DX

loop:
	VPMOVZXDQ	0(SI), Y0
	VPMOVZXDQ	16(SI), Y1
	VPMOVZXDQ	32(SI), Y2
	VPMOVZXDQ	48(SI), Y3
	VMOVQ	64(SI), X4
	VPMOVZXDQ	X4, Y4

	VPMULUDQ	0(DX), Y0, Y0
	VPMULUDQ	32(DX), Y1, Y1
	VPMULUDQ	64(DX), Y2, Y2
	VPMULUDQ	96(DX), Y3, Y3
	VPMULUDQ	128(DX), Y4, Y4
	VPADDQ	Y1, Y0, Y0
	VPADDQ	Y3, Y2, Y2
	VPADDQ	Y4, Y0, Y0
	VPADDQ	Y2, Y0, Y0

	VEXTRACTI128	$1, Y0, X1
	VPADDQ	X1, X0, X0
	VPSHUFD	$0x4e, X0, X1
	VPADDQ	X1, X0, X0
	VMOVQ	X0, 0(DI)

	ADDQ	$4, SI
	ADDQ	$8, DI
	DECQ	AX
	JNZ	loop

done:
	VZEROUPPER
	RET
