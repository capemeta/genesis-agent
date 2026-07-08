#!/usr/bin/env python3
"""Extract text from a PDF using pypdf or pdftotext fallback."""

import json
import os
import subprocess
import sys
import tempfile

from path_contract import resolve_input_path, tmp_dir


def with_pypdf(path):
    from pypdf import PdfReader

    reader = PdfReader(path)
    pages = []
    for idx, page in enumerate(reader.pages, start=1):
        pages.append({"page": idx, "text": page.extract_text() or ""})
    return pages


def with_pdftotext(path):
    with tempfile.TemporaryDirectory(dir=tmp_dir()) as tmp:
        out = os.path.join(tmp, "out.txt")
        subprocess.run(["pdftotext", "-layout", path, out], check=True, capture_output=True, text=True)
        with open(out, encoding="utf-8", errors="replace") as f:
            return [{"page": None, "text": f.read()}]


def main(argv):
    if len(argv) != 2:
        return {"ok": False, "errors": ["usage: extract_pdf_text.py <input.pdf>"]}
    path = resolve_input_path(argv[1])
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    try:
        try:
            pages = with_pypdf(path)
            engine = "pypdf"
        except ModuleNotFoundError:
            pages = with_pdftotext(path)
            engine = "pdftotext"
        text_chars = sum(len(p["text"]) for p in pages)
        return {
            "ok": True,
            "path": path,
            "engine": engine,
            "pages": len(pages),
            "text_chars": text_chars,
            "sample_text": "\n".join(p["text"] for p in pages)[:4000],
            "warnings": [] if text_chars else ["No extractable text found; OCR may be required."],
            "artifacts": [],
        }
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
