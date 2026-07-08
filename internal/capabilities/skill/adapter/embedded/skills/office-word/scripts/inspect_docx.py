#!/usr/bin/env python3
"""Inspect a DOCX package with only Python stdlib."""

import json
import os
import sys
import zipfile
import xml.etree.ElementTree as ET

from path_contract import resolve_input_path

NS = {"w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main"}


def text_of(node):
    return "".join(t.text or "" for t in node.findall(".//w:t", NS))


def inspect(path):
    with zipfile.ZipFile(path) as zf:
        names = set(zf.namelist())
        document_xml = zf.read("word/document.xml")
        root = ET.fromstring(document_xml)
        paragraphs = root.findall(".//w:p", NS)
        tables = root.findall(".//w:tbl", NS)
        images = [n for n in names if n.startswith("word/media/")]
        comments = "word/comments.xml" in names
        footnotes = "word/footnotes.xml" in names
        headers = [n for n in names if n.startswith("word/header")]
        footers = [n for n in names if n.startswith("word/footer")]
        sample = []
        for p in paragraphs:
            value = text_of(p).strip()
            if value:
                sample.append(value)
            if len(sample) >= 8:
                break
        return {
            "ok": True,
            "path": path,
            "size_bytes": os.path.getsize(path),
            "paragraphs": len(paragraphs),
            "tables": len(tables),
            "images": len(images),
            "comments": comments,
            "footnotes": footnotes,
            "headers": len(headers),
            "footers": len(footers),
            "sample_text": sample,
            "recommended_profile": "office-ocr" if images and not sample else "office-basic",
            "warnings": ["Images found with little text; OCR may be needed."] if images and not sample else [],
        }


def main(argv):
    if len(argv) != 2:
        return {"ok": False, "errors": ["usage: inspect_docx.py <input.docx>"]}
    path = resolve_input_path(argv[1])
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    try:
        return inspect(path)
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
