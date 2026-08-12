//go:build amd64

#include "textflag.h"

// func x86HasAVX2() bool
//
// AVX2 is usable iff CPUID.1:ECX has OSXSAVE(27)+AVX(28), XCR0 enables
// XMM+YMM state (bits 1-2), and CPUID.(7,0):EBX has AVX2(5).
TEXT ·x86HasAVX2(SB), NOSPLIT, $0-1
	MOVL	$1, AX
	XORL	CX, CX
	CPUID
	MOVL	CX, DX
	ANDL	$(1<<27 | 1<<28), DX
	CMPL	DX, $(1<<27 | 1<<28)
	JNE	no

	XORL	CX, CX
	XGETBV
	ANDL	$6, AX
	CMPL	AX, $6
	JNE	no

	MOVL	$7, AX
	XORL	CX, CX
	CPUID
	TESTL	$(1<<5), BX
	JZ	no

	MOVB	$1, ret+0(FP)
	RET
no:
	MOVB	$0, ret+0(FP)
	RET
