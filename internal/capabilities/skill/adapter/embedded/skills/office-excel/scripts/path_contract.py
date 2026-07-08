"""Helpers for Genesis execution workspace paths."""

import os


def logical_dir(name, fallback):
    return os.environ.get(name) or fallback


def input_dir():
    return logical_dir("INPUT_DIR", ".")


def output_dir():
    return logical_dir("OUTPUT_DIR", ".")


def tmp_dir():
    return logical_dir("TMPDIR", os.environ.get("TEMP") or ".")


def resolve_input_path(raw):
    if os.path.isabs(raw) and os.path.exists(raw):
        return raw
    candidates = [raw, os.path.join(input_dir(), raw), os.path.join(input_dir(), os.path.basename(raw))]
    for candidate in candidates:
        if candidate and os.path.exists(candidate):
            return candidate
    return candidates[1] if len(candidates) > 1 else raw


def resolve_output_path(raw, input_path):
    target = raw or os.path.join(output_dir(), os.path.basename(input_path))
    if not os.path.isabs(target):
        target = os.path.join(output_dir(), target)
    os.makedirs(os.path.dirname(target), exist_ok=True)
    return target
