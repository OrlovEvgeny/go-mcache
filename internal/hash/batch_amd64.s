// Copyright 2024 go-mcache authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.

//go:build amd64

#include "textflag.h"

// Constants for FNV-1a
#define FNV_OFFSET_64 $14695981039346656037
#define FNV_PRIME_64 $1099511628211

// Constants for splitmix64
#define SPLITMIX_C1 $0xbf58476d1ce4e5b9
#define SPLITMIX_C2 $0x94d049bb133111eb

// mix64Asm applies splitmix64 mixing to a single uint64.
// func mix64Asm(x uint64) uint64
TEXT ·mix64Asm(SB), NOSPLIT, $0-16
    MOVQ x+0(FP), AX

    // x ^= x >> 30
    MOVQ AX, BX
    SHRQ $30, BX
    XORQ BX, AX

    // x *= 0xbf58476d1ce4e5b9
    MOVQ SPLITMIX_C1, CX
    IMULQ CX, AX

    // x ^= x >> 27
    MOVQ AX, BX
    SHRQ $27, BX
    XORQ BX, AX

    // x *= 0x94d049bb133111eb
    MOVQ SPLITMIX_C2, CX
    IMULQ CX, AX

    // x ^= x >> 31
    MOVQ AX, BX
    SHRQ $31, BX
    XORQ BX, AX

    MOVQ AX, ret+8(FP)
    RET

// mix64x4Asm applies splitmix64 mixing to 4 uint64 values.
// func mix64x4Asm(x0, x1, x2, x3 uint64) (r0, r1, r2, r3 uint64)
TEXT ·mix64x4Asm(SB), NOSPLIT, $0-64
    // Load all 4 inputs
    MOVQ x0+0(FP), R8
    MOVQ x1+8(FP), R9
    MOVQ x2+16(FP), R10
    MOVQ x3+24(FP), R11

    // Stage 1: x ^= x >> 30 for all 4
    MOVQ R8, AX
    SHRQ $30, AX
    XORQ AX, R8

    MOVQ R9, AX
    SHRQ $30, AX
    XORQ AX, R9

    MOVQ R10, AX
    SHRQ $30, AX
    XORQ AX, R10

    MOVQ R11, AX
    SHRQ $30, AX
    XORQ AX, R11

    // Stage 2: x *= C1 for all 4
    MOVQ SPLITMIX_C1, CX
    IMULQ CX, R8
    IMULQ CX, R9
    IMULQ CX, R10
    IMULQ CX, R11

    // Stage 3: x ^= x >> 27 for all 4
    MOVQ R8, AX
    SHRQ $27, AX
    XORQ AX, R8

    MOVQ R9, AX
    SHRQ $27, AX
    XORQ AX, R9

    MOVQ R10, AX
    SHRQ $27, AX
    XORQ AX, R10

    MOVQ R11, AX
    SHRQ $27, AX
    XORQ AX, R11

    // Stage 4: x *= C2 for all 4
    MOVQ SPLITMIX_C2, CX
    IMULQ CX, R8
    IMULQ CX, R9
    IMULQ CX, R10
    IMULQ CX, R11

    // Stage 5: x ^= x >> 31 for all 4
    MOVQ R8, AX
    SHRQ $31, AX
    XORQ AX, R8

    MOVQ R9, AX
    SHRQ $31, AX
    XORQ AX, R9

    MOVQ R10, AX
    SHRQ $31, AX
    XORQ AX, R10

    MOVQ R11, AX
    SHRQ $31, AX
    XORQ AX, R11

    // Store results
    MOVQ R8, ret+32(FP)
    MOVQ R9, ret+40(FP)
    MOVQ R10, ret+48(FP)
    MOVQ R11, ret+56(FP)
    RET

// shardIndex computes shard index from hash.
// func shardIndex(hash, mask uint64) uint64
TEXT ·shardIndex(SB), NOSPLIT, $0-24
    MOVQ hash+0(FP), AX
    MOVQ mask+8(FP), BX
    ANDQ BX, AX
    MOVQ AX, ret+16(FP)
    RET
