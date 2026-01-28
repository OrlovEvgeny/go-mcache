// Copyright 2024 go-mcache authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.

//go:build amd64

#include "textflag.h"

// func PrefetchT0(addr unsafe.Pointer)
// Prefetch data into all cache levels (L1, L2, L3)
TEXT ·PrefetchT0(SB), NOSPLIT, $0-8
    MOVQ addr+0(FP), AX
    PREFETCHT0 (AX)
    RET

// func PrefetchT1(addr unsafe.Pointer)
// Prefetch data into L2 and L3 caches
TEXT ·PrefetchT1(SB), NOSPLIT, $0-8
    MOVQ addr+0(FP), AX
    PREFETCHT1 (AX)
    RET

// func PrefetchT2(addr unsafe.Pointer)
// Prefetch data into L3 cache
TEXT ·PrefetchT2(SB), NOSPLIT, $0-8
    MOVQ addr+0(FP), AX
    PREFETCHT2 (AX)
    RET

// func PrefetchNTA(addr unsafe.Pointer)
// Prefetch with non-temporal hint (minimize cache pollution)
TEXT ·PrefetchNTA(SB), NOSPLIT, $0-8
    MOVQ addr+0(FP), AX
    PREFETCHNTA (AX)
    RET

// func PrefetchW(addr unsafe.Pointer)
// Prefetch for write (brings cache line into exclusive state)
TEXT ·PrefetchW(SB), NOSPLIT, $0-8
    MOVQ addr+0(FP), AX
    PREFETCHW (AX)
    RET
