#!/usr/bin/env python3
"""Inspect a PPTX package with only Python stdlib."""

import os
import re
import sys
import zipfile
import xml.etree.ElementTree as ET

from path_contract import emit_json, resolve_input_path

NS = {"a": "http://schemas.openxmlformats.org/drawingml/2006/main"}
SLIDE_RE = re.compile(r"ppt/slides/slide(\d+)\.xml$")


def inspect(path):
    with zipfile.ZipFile(path) as zf:
        names = zf.namelist()
        slides = sorted((n for n in names if SLIDE_RE.match(n)), key=lambda n: int(SLIDE_RE.match(n).group(1)))
        media = [n for n in names if n.startswith("ppt/media/")]
        notes = [n for n in names if n.startswith("ppt/notesSlides/")]
        slide_summaries = []
        text_count = 0
        for slide in slides:
            root = ET.fromstring(zf.read(slide))
            texts = [t.text or "" for t in root.findall(".//a:t", NS)]
            joined = " ".join(x.strip() for x in texts if x.strip())
            if joined:
                text_count += 1
            slide_summaries.append({"slide": slide, "text": joined[:500]})
        warnings = []
        if media and text_count == 0:
            warnings.append("Slides contain media but no extractable text; OCR may be required.")
        return {
            "ok": True,
            "path": path,
            "size_bytes": os.path.getsize(path),
            "slides": len(slides),
            "media": len(media),
            "notes": len(notes),
            "slides_with_text": text_count,
            "sample_slides": slide_summaries[:5],
            "recommended_profile": "office-ocr" if media and text_count == 0 else "office-basic",
            "warnings": warnings,
        }


def main(argv):
    if len(argv) != 2:
        return {"ok": False, "errors": ["usage: inspect_pptx.py <input.pptx>"]}
    path = resolve_input_path(argv[1])
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    try:
        return inspect(path)
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    result = main(sys.argv)
    emit_json(result, exit_code=0 if result.get("ok") else 1)
