#!/usr/bin/env python3
"""Recalculate an XLSX workbook with LibreOffice and scan for common formula errors."""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import zipfile
import xml.etree.ElementTree as ET

from path_contract import resolve_input_path, resolve_output_path, tmp_dir

NS = {"main": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
ERRORS = {"#REF!", "#DIV/0!", "#VALUE!", "#N/A", "#NAME?", "#NUM!", "#NULL!"}


def scan_errors(path):
    found = []
    formulas = 0
    with zipfile.ZipFile(path) as zf:
        for name in zf.namelist():
            if not name.startswith("xl/worksheets/sheet") or not name.endswith(".xml"):
                continue
            root = ET.fromstring(zf.read(name))
            formulas += len(root.findall(".//main:f", NS))
            for cell in root.findall(".//main:c", NS):
                value = cell.find("main:v", NS)
                if value is not None and value.text in ERRORS:
                    found.append({"sheet_xml": name, "cell": cell.attrib.get("r", ""), "error": value.text})
    return formulas, found


def main(argv):
    if len(argv) < 2:
        return {"ok": False, "errors": ["usage: recalc_xlsx.py <input.xlsx> [output.xlsx] [timeout_seconds]"]}
    path = resolve_input_path(argv[1])
    output_path = None
    timeout = 60
    if len(argv) > 2:
        try:
            timeout = int(argv[2])
        except ValueError:
            output_path = argv[2]
    if len(argv) > 3:
        timeout = int(argv[3])
    output_path = resolve_output_path(output_path, path)
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    try:
        with tempfile.TemporaryDirectory(dir=tmp_dir()) as tmp:
            input_dir = os.path.join(tmp, "input")
            output_dir = os.path.join(tmp, "output")
            os.makedirs(input_dir, exist_ok=True)
            os.makedirs(output_dir, exist_ok=True)
            work = os.path.join(input_dir, os.path.basename(path))
            shutil.copy2(path, work)
            subprocess.run(["soffice", "--headless", "--convert-to", "xlsx", "--outdir", output_dir, work], check=True, timeout=timeout, capture_output=True, text=True)
            converted = os.path.join(output_dir, os.path.basename(path))
            if not os.path.exists(converted):
                candidates = [os.path.join(output_dir, item) for item in os.listdir(output_dir) if item.lower().endswith(".xlsx")]
                if not candidates:
                    raise RuntimeError("LibreOffice did not produce an xlsx output")
                converted = candidates[0]
            shutil.copy2(converted, output_path)
        formulas, errors = scan_errors(output_path)
        return {
            "ok": len(errors) == 0,
            "path": path,
            "output_path": output_path,
            "total_formulas": formulas,
            "total_errors": len(errors),
            "cell_errors": errors[:100],
            "artifacts": [{"path": output_path, "kind": "xlsx"}],
            "warnings": [] if not errors else ["Formula errors remain after recalculation."],
        }
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
