#!/usr/bin/env python3
"""Lightweight PDF inspection script for Genesis Office skills.

Outputs JSON so the agent can decide whether office-basic is enough or OCR is needed.
"""

import json
import os
import sys

from path_contract import resolve_input_path


def inspect_with_pypdf(path):
    from pypdf import PdfReader

    reader = PdfReader(path)
    pages = len(reader.pages)
    sample_parts = []
    pages_with_text = 0
    for page in reader.pages[: min(pages, 5)]:
        text = page.extract_text() or ""
        if text.strip():
            pages_with_text += 1
            sample_parts.append(text[:800])
    encrypted = bool(getattr(reader, "is_encrypted", False))
    return {
        "ok": True,
        "path": path,
        "size_bytes": os.path.getsize(path),
        "pages": pages,
        "encrypted": encrypted,
        "sample_text": "\n".join(sample_parts)[:2000],
        "text_layer_detected": pages_with_text > 0,
        "suspected_scanned": pages > 0 and pages_with_text == 0,
        "recommended_profile": "office-basic" if pages_with_text > 0 else "office-ocr",
        "warnings": [] if pages_with_text > 0 else ["No text layer detected in sampled pages; OCR may be required."],
    }


def fallback_inspect(path):
    with open(path, "rb") as f:
        header = f.read(8)
    return {
        "ok": True,
        "path": path,
        "size_bytes": os.path.getsize(path),
        "pages": None,
        "encrypted": None,
        "sample_text": "",
        "text_layer_detected": None,
        "suspected_scanned": None,
        "recommended_profile": "office-basic",
        "warnings": ["pypdf is unavailable; only PDF header was checked."],
        "diagnostics": {"header": header.decode("latin1", errors="replace")},
    }


def main(argv):
    if len(argv) != 2:
        return {"ok": False, "errors": ["usage: inspect_pdf.py <input.pdf>"]}
    path = resolve_input_path(argv[1])
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    try:
        try:
            return inspect_with_pypdf(path)
        except ModuleNotFoundError:
            return fallback_inspect(path)
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
