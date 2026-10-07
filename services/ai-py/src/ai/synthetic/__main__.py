"""`python -m ai.synthetic build [--check] [--root evals/datasets] [--dataset NAME ...]` (run in services/ai-py)."""

import argparse
import sys
from pathlib import Path

from ai.synthetic.datasets import DATASETS, build


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m ai.synthetic")
    sub = p.add_subparsers(dest="cmd", required=True)
    b = sub.add_parser("build", help="render the eval datasets and write (or --check) manifest.json/truth.jsonl")
    b.add_argument("--root", type=Path, default=Path("evals/datasets"))
    b.add_argument("--dataset", action="append", choices=sorted(DATASETS))
    b.add_argument("--check", action="store_true", help="fail when a regenerated binary or truth differs")
    args = p.parse_args(argv)
    problems = build(args.root, args.dataset, check=args.check)
    for line in problems:
        print(line, file=sys.stderr)
    print(f"{'checked' if args.check else 'built'} {', '.join(args.dataset or DATASETS)}: "
          f"{'FAILED' if problems else 'ok'}")
    return 1 if problems else 0


if __name__ == "__main__":
    raise SystemExit(main())
