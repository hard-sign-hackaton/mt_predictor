from __future__ import annotations

import argparse
import json
from pathlib import Path

from .builder import build_catalog, load_dataset, write_artifacts


def build_command(args: argparse.Namespace) -> int:
    bundle = load_dataset(args.dataset_dir)
    catalog, bindings, quality = build_catalog(bundle)
    write_artifacts(catalog, bindings, quality, args.output_dir)
    print(json.dumps(quality, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description="Build derived route catalog for MT Predictor")
    commands = root.add_subparsers(dest="command", required=True)
    build = commands.add_parser("build", help="extract mappings, stops, route patterns and geometry")
    build.add_argument("--dataset-dir", type=Path, required=True)
    build.add_argument("--output-dir", type=Path, required=True)
    build.set_defaults(handler=build_command)
    return root


def main() -> int:
    args = parser().parse_args()
    return args.handler(args)


if __name__ == "__main__":
    raise SystemExit(main())

