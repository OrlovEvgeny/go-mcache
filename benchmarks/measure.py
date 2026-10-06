#!/usr/bin/env python3
"""Interleave prebuilt benchmark binaries on one otherwise idle physical host."""
import argparse
import os
import subprocess
from contextlib import ExitStack
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("binaries", nargs="+", help="NAME=/absolute/path/to/test-binary")
parser.add_argument("--output", type=Path, default=Path("benchmarks"))
parser.add_argument("--arch", required=True, choices=["arm64", "amd64"])
parser.add_argument("--cpu", default="8", help="GOMAXPROCS for every binary")
parser.add_argument("--count", type=int, default=10)
parser.add_argument("--time", default="300ms")
parser.add_argument("--bench", default="Benchmark(Cache(Get|Set|SetWithTTL|Mixed|Zipf)$|BatchSizes|TTLUnique|TTLOverwriteSerial)")
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
variants = [item.split("=", 1) for item in args.binaries]
env = dict(os.environ, GOMAXPROCS=args.cpu)
with ExitStack() as stack:
    outputs = {name: stack.enter_context((args.output / f"{name}-{args.arch}.txt").open("w"))
               for name, _ in variants}
    for repetition in range(args.count):
        for name, binary in (variants if repetition % 2 == 0 else variants[::-1]):
            subprocess.run([binary, "-test.run=^$", f"-test.bench={args.bench}",
                            "-test.benchmem", f"-test.benchtime={args.time}", "-test.count=1"],
                           env=env, stdout=outputs[name], check=True)
            outputs[name].flush()
        print(f"Sample {repetition + 1}/{args.count} complete", flush=True)
