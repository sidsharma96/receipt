#!/usr/bin/env python3
"""Compare direct execution, a Receipt miss and repeated hits in an external cache.

Runs the command the operator selects; it may have its normal side effects.
Never edits repository configuration or installs dependencies. Output excludes
child output and environment values. Keep the command identical across samples.
"""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile
import time


def measure(command, cwd, kind):
    started = time.perf_counter_ns()
    result = subprocess.run(command, cwd=cwd, capture_output=True, timeout=120)
    sample = {"kind": kind, "client_ns": time.perf_counter_ns()-started,
              "exit_code": result.returncode}
    if kind != "direct":
        lines = result.stderr.splitlines()
        if not lines:
            raise RuntimeError("missing timing summary")
        summary = json.loads(lines[-1])
        if summary.get("type") != "receipt_timing":
            raise RuntimeError("missing final Receipt timing summary")
        sample["stages"] = summary["stages"]
        sample["receipt_ns"] = summary["duration_ns"]
        sample["executed"] = any(t["stage"] == "execution" for t in summary["stages"])
        if kind == "hit" and sample["executed"]:
            raise RuntimeError("expected a hit but Receipt executed; inspect inputs separately")
    if result.returncode:
        raise RuntimeError(f"{kind} exited {result.returncode}; no performance claim recorded")
    return sample


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", required=True, type=Path)
    parser.add_argument("--receipt", required=True, type=Path)
    parser.add_argument("--repeats", type=int, default=5)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command or not 1 <= args.repeats <= 100:
        parser.error("provide a command and 1–100 repeats")
    samples = [measure(command, args.repo, "direct") for _ in range(args.repeats)]
    with tempfile.TemporaryDirectory(prefix="receipt-measure-") as cache:
        wrapped = [str(args.receipt.resolve()), "run", "--timings", "--cache-dir", cache, "--"] + command
        samples.append(measure(wrapped, args.repo, "miss"))
        samples.extend(measure(wrapped, args.repo, "hit") for _ in range(args.repeats))
    print(json.dumps({"samples": samples, "scope": "one selected command; warm OS caches; no edits between runs"}, indent=2))


if __name__ == "__main__":
    main()
