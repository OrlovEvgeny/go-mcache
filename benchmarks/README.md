# Batch and SIMD measurements

The accepted changes compute batch hashes once, hold one read lock per sorted
shard group, and write directly into caller-owned result arrays. Only numeric
scratch enters the pool. Optional SIMD classifies private deadline arrays in
batch reads and background expiration; it never writes TinyLFU counters.
The existing cache API, policies, metrics, and cached clock remain in use.

**The proposed TTL/Roaring scheduler is excluded.** Candidate `f249e2b` passed
correctness tests but regressed sustained sequential TTL overwrites beyond the
5% limit: 51.74 to 59.73 ns/op (+15.45%, p < 0.001, n=10). Earlier TTL prototypes improved parallel throughput and retained less memory,
but the final candidate still failed the regression gate. The original scheduler remains,
with a correction for exact deadline equality. No overall TTL speedup is claimed.
See [the rejected candidate's measurements](rejected-ttl/comparison-arm64.txt).

Measurements use Go 1.27.1, darwin/arm64, Apple M4 Pro, GOMAXPROCS=8, on
October 6, 2026. The baseline is `a7dcf35`, with `perf_serial_test.go` copied in
without changing library code. Each latency workload has ten 300 ms samples,
alternating baseline, scalar, and SIMD binaries on the same physical host.

Native amd64 tests and measurements were unavailable locally. Both modes
cross-compile for Linux amd64. CI now runs ordinary and SIMD build/vet/tests/race
on Linux amd64, Linux arm64, and macOS, plus forced SIMD emulation. See
[GitHub's runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
CI performance from different hosts must not be compared for acceptance.

## Results on arm64

Medians of ten samples. Every `GetBatchOptimized` scalar improvement below is
significant at p < 0.001; the full reports include confidence intervals.

| Batch size | Baseline | Scalar | Change | SIMD |
|---:|---:|---:|---:|---:|
| 1 | 61.91 ns | 50.61 ns | -18.25% | 50.77 ns |
| 16 | 476.4 ns | 347.7 ns | -27.01% | 343.1 ns |
| 64 | 1.974 us | 1.495 us | -24.27% | 1.427 us |
| 256 | 8.995 us | 6.935 us | -22.91% | 6.619 us |
| 1024 | 39.86 us | 31.49 us | -20.99% | 30.54 us |

At 1024 keys, optimized batch allocation falls from 59.34 KiB to 17.12 KiB
per call (-71.16%), and from 7 to 4 allocations. The ordinary batch API also
uses 4 allocations; its large-batch scalar latency has no significant change.
SIMD reduces its 1024-key latency by 8.27% relative to the new scalar build.

Get, Set, parallel TTL writes, sequential TTL writes, unique TTL inserts, and
Zipf latency show no significant change against baseline. No confirmed >5%
regression occurs in this cache-level matrix. Zipf hit ratio is 87.46% before
and 87.47% after (p=0.529). Retained memory is unchanged statistically:
1.251 MiB after one fill, and 27.66 vs 27.44 MiB after 100 overwrite rounds.
The TTL overwrite speedup target was **not achieved** in the accepted version.

The SIMD kernel speeds up 64/256/1024/16384-item classification by
10.18%/27.68%/20.96%/12.07%. At 16 items, dispatch increases the standalone
scalar-fallback call from 6.362 to 10.095 ns; cache-level small batches show no
significant SIMD regression. SIMD is optional, not the default.

Reports: [baseline vs scalar](comparison-arm64.txt),
[scalar vs SIMD](comparison-simd-arm64.txt),
[retained memory](comparison-memory-arm64.txt),
[kernel](comparison-kernel-arm64.txt).

Local validation passed: ordinary and SIMD build, vet, full tests with race
detection, forced emulation tests, and Linux amd64 cross-builds. The CI matrix
is configured but has not run remotely. Native amd64 performance acceptance
remains pending.

## Reproduce

Build all versions with the same toolchain. Stop other CPU-heavy work.

```sh
go install golang.org/x/perf/cmd/benchstat@v0.0.0-20260929162123-406019bb8b68
git worktree add --detach /tmp/mcache-before a7dcf35
cp perf_serial_test.go /tmp/mcache-before/
(cd /tmp/mcache-before && go test -c -o /tmp/mcache-baseline.test)
go test -c -o /tmp/mcache-after.test
GOEXPERIMENT=simd go test -c -o /tmp/mcache-simd.test
python3 benchmarks/measure.py --arch arm64 \
  baseline=/tmp/mcache-baseline.test \
  after=/tmp/mcache-after.test \
  simd=/tmp/mcache-simd.test
benchstat benchmarks/baseline-arm64.txt benchmarks/after-arm64.txt
benchstat benchmarks/after-arm64.txt benchmarks/simd-arm64.txt
```

Repeat on physical amd64 hardware with `--arch amd64`. Keep Go, GOMAXPROCS,
power settings, and duration identical for every binary. Use `--time 3s` for a
longer confirmation before release.

`TTLUnique` includes Clear plus 10,000 unique inserts per operation.
`CacheSetWithTTL` measures parallel overwrites of a 10,000-key working set;
`TTLOverwriteSerial` measures sustained sequential overwrites separately.
`BatchSizes` covers 1/16/64/256/1024 hits with TTL. Correctness tests cover mixed
hits, misses, expiration, collisions, ordering, duplicates, metrics, and access
recording for both policies. `CacheZipf` reports hit ratio with the unchanged
admission and eviction implementation.

Retained-memory diagnostics populate 10,000 live integer entries with one-hour
TTL, once or for 100 overwrite rounds. They force GC before and after while
keeping the cache reachable. Their elapsed times include construction and GC;
use the dedicated overwrite benchmarks to assess operation latency.

```sh
GOMAXPROCS=8 /tmp/mcache-baseline.test -test.run '^$' \
  -test.bench BenchmarkTTLRetained -test.benchtime=1x -test.count=10
GOMAXPROCS=8 /tmp/mcache-after.test -test.run '^$' \
  -test.bench BenchmarkTTLRetained -test.benchtime=1x -test.count=10
```

The SIMD kernel can be measured separately:

```sh
GOMAXPROCS=8 go test ./internal/store -run '^$' \
  -bench BenchmarkExpiryKernel -benchmem -benchtime=200ms -count=10
GOMAXPROCS=8 GOEXPERIMENT=simd go test ./internal/store -run '^$' \
  -bench BenchmarkExpiryKernel -benchmem -benchtime=200ms -count=10
GOEXPERIMENT=simd GODEBUG=simd=0 go test ./...
```

The [experimental standard simd package](https://pkg.go.dev/simd) is opt-in.
The loop processes four vectors at a time and handles tails scalarly. Below 64
items, and under emulation, it uses the scalar algorithm. A standalone 16-item
call still pays dispatch overhead in the SIMD build; this is reported in the
kernel comparison and is not presented as an improvement.

## Reproduce the rejected TTL candidate

```sh
git worktree add --detach /tmp/mcache-ttl-candidate f249e2b
(cd /tmp/mcache-ttl-candidate && go test -c -o /tmp/mcache-ttl.test)
python3 benchmarks/measure.py --arch arm64 \
  --bench BenchmarkTTLOverwriteSerial --output benchmarks/rejected-ttl \
  baseline=/tmp/mcache-baseline.test candidate=/tmp/mcache-ttl.test
benchstat benchmarks/rejected-ttl/baseline-arm64.txt \
  benchmarks/rejected-ttl/candidate-arm64.txt
```

The candidate's bitmap mutation and iteration were protected by owner shard
locks, following [Roaring's requirements](https://github.com/RoaringBitmap/roaring#goroutine-safety).
The dependency is absent from the accepted version.

Other verified fixes: iterators drain buffered entries after the final scan;
Clear updates store size while holding each shard lock; expiration callbacks
run after the cleanup lock is released, allowing them to call Clear.
