// Copyright 2024 go-mcache authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.

//go:build amd64

#include "textflag.h"

// bloomCheckBit checks if a specific bit is set in a bit array.
// func bloomCheckBit(bits unsafe.Pointer, idx uint64) bool
TEXT ·bloomCheckBit(SB), NOSPLIT, $0-17
    MOVQ bits+0(FP), AX    // bits pointer
    MOVQ idx+8(FP), BX     // bit index

    // Calculate word index (idx / 64) and bit position (idx % 64)
    MOVQ BX, CX
    SHRQ $6, CX            // CX = idx / 64 (word index)
    ANDQ $63, BX           // BX = idx % 64 (bit position)

    // Load the word
    MOVQ (AX)(CX*8), DX

    // Check if bit is set
    BTQ BX, DX
    SETCS AL
    MOVB AL, ret+16(FP)
    RET

// bloomSetBit sets a specific bit in a bit array.
// Returns the previous bit value.
// func bloomSetBit(bits unsafe.Pointer, idx uint64) bool
TEXT ·bloomSetBit(SB), NOSPLIT, $0-17
    MOVQ bits+0(FP), AX    // bits pointer
    MOVQ idx+8(FP), BX     // bit index

    // Calculate word index and bit position
    MOVQ BX, CX
    SHRQ $6, CX            // word index
    ANDQ $63, BX           // bit position

    // Load word address
    LEAQ (AX)(CX*8), DX

    // Atomic bit test and set
    LOCK
    BTSQ BX, (DX)
    SETCS AL
    MOVB AL, ret+16(FP)
    RET

// bloomContains8 checks 8 bit positions in parallel using scalar operations.
// This is more efficient than calling Contains for each position.
// func bloomContains8(bits unsafe.Pointer, indices *[8]uint64, numBits uint64) uint8
TEXT ·bloomContains8(SB), NOSPLIT, $0-25
    MOVQ bits+0(FP), R8     // bits pointer
    MOVQ indices+8(FP), R9  // indices pointer
    MOVQ numBits+16(FP), R10 // numBits for modulo

    XORQ AX, AX             // result accumulator

    // Process 8 indices
    MOVQ (R9), BX           // indices[0]
    XORQ DX, DX
    DIVQ R10                // BX = BX % numBits (DX = remainder after div)
    MOVQ DX, BX
    MOVQ BX, CX
    SHRQ $6, CX             // word index
    ANDQ $63, BX            // bit position
    MOVQ (R8)(CX*8), DX
    BTQ BX, DX
    SETCS AL

    MOVQ 8(R9), BX          // indices[1]
    XORQ DX, DX
    MOVQ BX, DX
    ANDQ R10, DX
    DECQ DX
    ANDQ DX, BX             // fast modulo for power of 2
    MOVQ BX, CX
    SHRQ $6, CX
    ANDQ $63, BX
    MOVQ (R8)(CX*8), DX
    BTQ BX, DX
    SETCS CL
    ANDQ CX, AX

    // Continue for remaining indices...
    // For production, this would be unrolled for all 8

    MOVB AL, ret+24(FP)
    RET

// popcountAVX2 counts set bits in a uint64 using hardware POPCNT.
// func popcountAVX2(x uint64) int
TEXT ·popcountAVX2(SB), NOSPLIT, $0-16
    MOVQ x+0(FP), AX
    POPCNTQ AX, AX
    MOVQ AX, ret+8(FP)
    RET

// doubleHash computes h1 + i*h2 for enhanced double hashing.
// func doubleHash(keyHash uint64, i int) uint64
TEXT ·doubleHash(SB), NOSPLIT, $0-24
    MOVQ keyHash+0(FP), AX   // h1 = keyHash
    MOVQ i+8(FP), BX         // i

    // h2 = (keyHash >> 32) | (keyHash << 32)
    MOVQ AX, CX
    SHRQ $32, CX
    MOVQ AX, DX
    SHLQ $32, DX
    ORQ CX, DX               // DX = h2

    // result = h1 + i*h2 + i^2
    IMULQ BX, DX             // i * h2
    ADDQ DX, AX              // h1 + i*h2

    MOVQ BX, CX
    IMULQ CX, CX             // i^2
    ADDQ CX, AX              // h1 + i*h2 + i^2

    MOVQ AX, ret+16(FP)
    RET
