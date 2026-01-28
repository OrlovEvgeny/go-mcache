// Copyright 2024 go-mcache authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.

//go:build amd64

#include "textflag.h"

// cmEstimateMinAVX2 finds minimum across 4 uint8 values at specified indices.
// This is a helper for SIMD-accelerated Count-Min Sketch estimation.
//
// func cmEstimateMinAVX2(row0, row1, row2, row3 unsafe.Pointer, idx0, idx1, idx2, idx3 uint64) uint8
TEXT ·cmEstimateMinAVX2(SB), NOSPLIT, $0-72
    // Load row pointers
    MOVQ row0+0(FP), R8
    MOVQ row1+8(FP), R9
    MOVQ row2+16(FP), R10
    MOVQ row3+24(FP), R11

    // Load indices
    MOVQ idx0+32(FP), R12
    MOVQ idx1+40(FP), R13
    MOVQ idx2+48(FP), R14
    MOVQ idx3+56(FP), R15

    // Load bytes from each row
    MOVBQZX (R8)(R12*1), AX    // row0[idx0]
    MOVBQZX (R9)(R13*1), BX    // row1[idx1]
    MOVBQZX (R10)(R14*1), CX   // row2[idx2]
    MOVBQZX (R11)(R15*1), DX   // row3[idx3]

    // Find minimum of 4 values
    CMPQ BX, AX
    CMOVQLT BX, AX
    CMPQ CX, AX
    CMOVQLT CX, AX
    CMPQ DX, AX
    CMOVQLT DX, AX

    MOVB AL, ret+64(FP)
    RET

// cmIncrementSaturatingAVX2 increments a uint8 counter with saturation at 255.
// func cmIncrementSaturatingAVX2(ptr unsafe.Pointer, idx uint64) uint8
TEXT ·cmIncrementSaturatingAVX2(SB), NOSPLIT, $0-24
    MOVQ ptr+0(FP), AX
    MOVQ idx+8(FP), BX

    // Load current value
    MOVBQZX (AX)(BX*1), CX

    // Check if already at max
    CMPQ CX, $255
    JGE  already_max

    // Increment
    INCQ CX

    // Store back
    MOVB CL, (AX)(BX*1)

already_max:
    MOVB CL, ret+16(FP)
    RET

// hashMix64 performs fast hash mixing (splitmix64 finalizer).
// func hashMix64(x uint64) uint64
TEXT ·hashMix64(SB), NOSPLIT, $0-16
    MOVQ x+0(FP), AX

    // x ^= x >> 33
    MOVQ AX, BX
    SHRQ $33, BX
    XORQ BX, AX

    // x *= 0xff51afd7ed558ccd
    MOVQ $0xff51afd7ed558ccd, CX
    IMULQ CX, AX

    // x ^= x >> 33
    MOVQ AX, BX
    SHRQ $33, BX
    XORQ BX, AX

    // x *= 0xc4ceb9fe1a85ec53
    MOVQ $0xc4ceb9fe1a85ec53, CX
    IMULQ CX, AX

    // x ^= x >> 33
    MOVQ AX, BX
    SHRQ $33, BX
    XORQ BX, AX

    MOVQ AX, ret+8(FP)
    RET
