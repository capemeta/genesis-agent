#!/usr/bin/env python3
"""List fillable PDF form fields."""

import json
import os
import sys

from path_contract import resolve_input_path


def main(argv):
    if len(argv) != 2:
        return {"ok": False, "errors": ["usage: list_pdf_form_fields.py <input.pdf>"]}
    path = resolve_input_path(argv[1])
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    try:
        from pypdf import PdfReader

        reader = PdfReader(path)
        fields = reader.get_fields() or {}
        out = []
        for name, field in fields.items():
            out.append({
                "name": name,
                "type": str(field.get("/FT", "")),
                "value": str(field.get("/V", "")),
                "required": bool(int(field.get("/Ff", 0)) & 2) if str(field.get("/Ff", "")).isdigit() else False,
            })
        return {"ok": True, "path": path, "field_count": len(out), "fields": out, "artifacts": []}
    except ModuleNotFoundError:
        return {"ok": False, "path": path, "errors": ["pypdf is required to inspect PDF form fields"]}
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
