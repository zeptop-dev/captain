#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Check the locale files of one or more i18n directories.

Fails when a locale is missing keys (or has empty values) compared with the
union of all locales in the same directory, and when the sources next to
them use a literal key that no locale defines (that renders the raw key in
the interface). Keys no source mentions are only reported, because keys can
also be built at run time (`t('plan.' + kind)`).

    python3 scripts/i18n-check.py web/admin/src/i18n [more dirs...]
"""
import json, os, re, sys

def flatten(d, prefix=""):
    out = {}
    for k, v in d.items():
        key = prefix + k
        if isinstance(v, dict):
            out.update(flatten(v, key + "."))
        else:
            out[key] = v
    return out

# t('a.b'), t("a.b"), i18n.t('a.b'), and the ones passed as a prop.
USE = re.compile(r"""\bt\(\s*['"]([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+)['"]""")
# A key built at run time: t('plan.' + kind) or t(`plan.${kind}`).
DYN = re.compile(r"""\bt\(\s*['"`]([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)*\.)['"`]?\s*(?:\+|\$\{)""")

def sources(d):
    """The source tree the locale directory belongs to (its parent)."""
    root = os.path.dirname(os.path.abspath(d))
    for base, _, files in os.walk(root):
        if "node_modules" in base:
            continue
        for f in files:
            if f.endswith((".ts", ".tsx", ".js", ".jsx")):
                yield os.path.join(base, f)

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
    used, prefixes = set(), set()
    for path in sources(d):
        text = open(path, encoding="utf-8").read()
        used |= set(USE.findall(text))
        prefixes |= set(DYN.findall(text))
    undefined = sorted(k for k in used - union)
    if undefined:
        bad += 1
        print(f"{d}: {len(undefined)} keys used by the sources but defined nowhere")
        for k in undefined[:20]:
            print("   ", k)
    unused = sorted(
        k for k in union - used
        if not any(k.startswith(p) for p in prefixes)
    )
    if unused:
        print(f"{d}: {len(unused)} keys no source mentions (delete or check for a run-time key)")
        for k in unused[:40]:
            print("   ", k)
sys.exit(1 if bad else 0)
