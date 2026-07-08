#!/usr/bin/env python3
"""Convert DOC/DOCX to PDF with LibreOffice headless."""

import json
import os
import subprocess
import sys

from path_contract import resolve_input_path, resolve_output_dir


def main(argv):
    if len(argv) < 2:
        return {"ok": False, "errors": ["usage: convert_docx_to_pdf.py <input.docx|doc> [output_dir]"]}
    path = resolve_input_path(argv[1])
    out_dir = resolve_output_dir(argv[2] if len(argv) > 2 else None)
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    os.makedirs(out_dir, exist_ok=True)
    try:
        subprocess.run(["soffice", "--headless", "--convert-to", "pdf", "--outdir", out_dir, path], check=True, capture_output=True, text=True)
        pdf_name = os.path.splitext(os.path.basename(path))[0] + ".pdf"
        pdf_path = os.path.join(out_dir, pdf_name)
        return {
            "ok": os.path.exists(pdf_path),
            "path": path,
            "output_pdf": pdf_path,
            "artifacts": [{"path": pdf_path, "kind": "pdf"}] if os.path.exists(pdf_path) else [],
            "warnings": [] if os.path.exists(pdf_path) else ["LibreOffice completed but output PDF was not found."],
        }
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
