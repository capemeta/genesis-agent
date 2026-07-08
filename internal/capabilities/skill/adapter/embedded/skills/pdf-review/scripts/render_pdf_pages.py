#!/usr/bin/env python3
"""Render PDF pages to images with pdftoppm."""

import json
import os
import subprocess
import sys

from path_contract import resolve_input_path, resolve_output_dir


def main(argv):
    if len(argv) < 2:
        return {"ok": False, "errors": ["usage: render_pdf_pages.py <input.pdf> [output_dir] [dpi]"]}
    path = resolve_input_path(argv[1])
    out_arg = argv[2] if len(argv) > 2 and not argv[2].isdigit() else None
    dpi = argv[3] if len(argv) > 3 else (argv[2] if len(argv) > 2 and argv[2].isdigit() else "150")
    out_dir = resolve_output_dir(out_arg)
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    os.makedirs(out_dir, exist_ok=True)
    prefix = os.path.join(out_dir, "page")
    try:
        subprocess.run(["pdftoppm", "-png", "-r", dpi, path, prefix], check=True, capture_output=True, text=True)
        files = sorted(os.path.join(out_dir, f) for f in os.listdir(out_dir) if f.startswith("page") and f.endswith(".png"))
        return {
            "ok": True,
            "path": path,
            "output_dir": out_dir,
            "pages_rendered": len(files),
            "artifacts": [{"path": f, "kind": "preview_image"} for f in files],
            "warnings": [] if files else ["No preview images were generated."],
        }
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
