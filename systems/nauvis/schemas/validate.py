#!/usr/bin/env python3
"""Validate Crossref 'work' JSON against crossref-work.schema.json.

Handles three shapes:
  * JSONL (one work per line)  -> out/*.json
  * a single work object       -> 10.json
  * an API response envelope   -> {"items": [work, ...]}  (data/*.json.gz)

Usage:
  uv run --with "jsonschema>=4" python validate.py [schema.json] <file> [file ...]
Exit code 0 if every work validates, 1 otherwise.
"""
import sys, json, collections
from jsonschema import Draft202012Validator

DEFAULT_SCHEMA = "crossref-work.schema.json"


def load_objects(path):
    """Yield (label, work) for each work in the file."""
    if path.endswith(".gz"):
        import gzip
        with gzip.open(path, "rt", encoding="utf-8") as f:
            text = f.read()
    else:
        with open(path, encoding="utf-8") as f:
            text = f.read()

    # single JSON document
    try:
        doc = json.loads(text)
    except json.JSONDecodeError:
        doc = None
    if doc is not None:
        if isinstance(doc, dict) and isinstance(doc.get("items"), list):
            for i, w in enumerate(doc["items"]):
                yield f"items[{i}]", w
            return
        yield "$root", doc
        return

    # JSONL: stream top-level objects
    dec = json.JSONDecoder()
    idx, L, n = 0, len(text), 0
    while idx < L:
        while idx < L and text[idx] in " \t\r\n":
            idx += 1
        if idx >= L:
            break
        obj, idx = dec.raw_decode(text, idx)
        n += 1
        yield f"line {n}", obj


def main():
    args = sys.argv[1:]
    schema_path = DEFAULT_SCHEMA
    files = args
    # optional explicit schema path as first arg (not a file that exists -> assume default)
    if args and not args[0].endswith(".json.gz") and not (args[0].startswith("out/") or args[0].startswith("data/")):
        import os
        if os.path.exists(args[0]):
            schema_path, files = args[0], args[1:]
    if not files:
        print("usage: validate.py <file> [file ...]   (schema defaults to crossref-work.schema.json)")
        sys.exit(2)

    with open(schema_path, encoding="utf-8") as f:
        schema = json.load(f)
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema)

    grand_total = grand_bad = 0
    shown = 0
    sigs = collections.Counter()
    for path in files:
        ok = bad = 0
        for label, obj in load_objects(path):
            ok += 1
            errs = list(validator.iter_errors(obj))
            if errs:
                bad += 1
                for e in errs:
                    tail = "/".join(str(x) for x in list(e.absolute_path)[-2:]) or "$"
                    sigs[f"{tail} :: {e.validator}"] += 1
                if shown < 25:
                    shown += 1
                    e = errs[0]
                    ppath = "/".join(str(x) for x in e.absolute_path)
                    print(f"FAIL {path}:{label}  /{ppath}: {e.message[:220]}")
        grand_total += ok
        grand_bad += bad
        print(f"[{path}] {ok} works, {bad} failed")
    print(f"\nTOTAL {grand_total} works, {grand_bad} failed")
    if sigs:
        print("\n== error categories (path :: validator) ==")
        for sig, cnt in sigs.most_common(30):
            print(f"  {cnt:6d}  {sig}")
    sys.exit(1 if grand_bad else 0)


if __name__ == "__main__":
    main()
