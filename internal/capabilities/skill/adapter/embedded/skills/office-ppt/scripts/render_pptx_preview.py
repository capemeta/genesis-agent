#!/usr/bin/env python3
"""Convert PPTX to PDF and render slide preview images."""

import json
import os
import subprocess
import sys

from path_contract import resolve_input_path, resolve_output_dir


def main(argv):
    if len(argv) < 2:
        return {"ok": False, "errors": ["usage: render_pptx_preview.py <input.pptx> [output_dir] [dpi]"]}
    path = resolve_input_path(argv[1])
    out_arg = argv[2] if len(argv) > 2 and not argv[2].isdigit() else None
    dpi = argv[3] if len(argv) > 3 else (argv[2] if len(argv) > 2 and argv[2].isdigit() else "150")
    out_dir = resolve_output_dir(out_arg)
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    os.makedirs(out_dir, exist_ok=True)
    try:
        subprocess.run(["soffice", "--headless", "--convert-to", "pdf", "--outdir", out_dir, path], check=True, capture_output=True, text=True)
        pdf_path = os.path.join(out_dir, os.path.splitext(os.path.basename(path))[0] + ".pdf")
        prefix = os.path.join(out_dir, "slide")
        subprocess.run(["pdftoppm", "-png", "-r", dpi, pdf_path, prefix], check=True, capture_output=True, text=True)
        images = sorted(os.path.join(out_dir, f) for f in os.listdir(out_dir) if f.startswith("slide") and f.endswith(".png"))
        return {
            "ok": True,
            "path": path,
            "output_pdf": pdf_path,
            "slides_rendered": len(images),
            "artifacts": [{"path": pdf_path, "kind": "pdf"}] + [{"path": f, "kind": "slide_preview"} for f in images],
            "warnings": [] if images else ["No slide preview images were generated."],
        }
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
