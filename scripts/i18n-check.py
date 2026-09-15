#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Fail when a locale file is missing keys (or has empty values) compared
with the union of all locales in the same directory. Usage:
    python3 scripts/i18n-check.py web/admin/src/i18n [more dirs...]
"""
import json, os, sys

def flatten(d, prefix=""):
    out = {}
    for k, v in d.items():
        key = prefix + k
        if isinstance(v, dict):
            out.update(flatten(v, key + "."))
        else:
            out[key] = v
    return out

bad = 0
for d in sys.argv[1:]:
    files = sorted(f for f in os.listdir(d) if f.endswith(".json"))
    flat = {f: flatten(json.load(open(os.path.join(d, f), encoding="utf-8"))) for f in files}
    union = set()
    for keys in flat.values():
        union |= set(keys)
    for f in files:
        missing = sorted(union - set(flat[f]))
        empty = sorted(k for k, v in flat[f].items() if v == "")
        if missing or empty:
            bad += 1
            print(f"{d}/{f}: missing {len(missing)}, empty {len(empty)}")
            for k in (missing + empty)[:20]:
                print("   ", k)
        else:
            print(f"{d}/{f}: ok ({len(flat[f])} keys)")
sys.exit(1 if bad else 0)
