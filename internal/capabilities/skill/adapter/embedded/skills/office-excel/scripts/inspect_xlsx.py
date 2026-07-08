#!/usr/bin/env python3
"""Inspect XLSX/CSV/TSV files and output JSON."""

import csv
import json
import os
import sys
import zipfile
import xml.etree.ElementTree as ET

from path_contract import resolve_input_path

NS = {
    "main": "http://schemas.openxmlformats.org/spreadsheetml/2006/main",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
}


def inspect_delimited(path, delimiter):
    rows = []
    with open(path, newline="", encoding="utf-8-sig") as f:
        reader = csv.reader(f, delimiter=delimiter)
        for row in reader:
            rows.append(row)
            if len(rows) >= 6:
                break
    return {
        "ok": True,
        "path": path,
        "size_bytes": os.path.getsize(path),
        "format": "tsv" if delimiter == "\t" else "csv",
        "sample_rows": rows,
        "columns": max((len(r) for r in rows), default=0),
        "recommended_profile": "office-basic",
        "warnings": [],
    }


def inspect_xlsx(path):
    with zipfile.ZipFile(path) as zf:
        names = set(zf.namelist())
        workbook = ET.fromstring(zf.read("xl/workbook.xml"))
        sheets = []
        for sheet in workbook.findall(".//main:sheet", NS):
            sheets.append(sheet.attrib.get("name", ""))
        sheet_files = [n for n in names if n.startswith("xl/worksheets/sheet") and n.endswith(".xml")]
        media = [n for n in names if n.startswith("xl/media/")]
        formulas = 0
        non_empty_cells = 0
        error_literals = []
        for filename in sheet_files[:10]:
            root = ET.fromstring(zf.read(filename))
            formulas += len(root.findall(".//main:f", NS))
            for cell in root.findall(".//main:c", NS):
                if cell.find("main:v", NS) is not None or cell.find("main:is", NS) is not None:
                    non_empty_cells += 1
                if cell.attrib.get("t") == "e":
                    ref = cell.attrib.get("r", "")
                    value = cell.find("main:v", NS)
                    error_literals.append({"cell": ref, "value": value.text if value is not None else ""})
        warnings = []
        if error_literals:
            warnings.append("Formula or cell errors found; recalculate and fix before delivery.")
        if media and non_empty_cells == 0:
            warnings.append("Embedded media found with no non-empty sampled cells; OCR may be required.")
        return {
            "ok": True,
            "path": path,
            "size_bytes": os.path.getsize(path),
            "format": "xlsx",
            "sheets": sheets,
            "sheet_count": len(sheets),
            "non_empty_cells_sampled": non_empty_cells,
            "formulas": formulas,
            "cell_errors": error_literals[:20],
            "media": len(media),
            "recommended_profile": "office-ocr" if media and non_empty_cells == 0 else "office-basic",
            "warnings": warnings,
        }


def main(argv):
    if len(argv) != 2:
        return {"ok": False, "errors": ["usage: inspect_xlsx.py <input.xlsx|csv|tsv>"]}
    path = resolve_input_path(argv[1])
    if not os.path.exists(path):
        return {"ok": False, "errors": [f"file not found: {path}"]}
    ext = os.path.splitext(path)[1].lower()
    try:
        if ext == ".csv":
            return inspect_delimited(path, ",")
        if ext == ".tsv":
            return inspect_delimited(path, "\t")
        return inspect_xlsx(path)
    except Exception as exc:
        return {"ok": False, "path": path, "errors": [str(exc)]}


if __name__ == "__main__":
    print(json.dumps(main(sys.argv), ensure_ascii=False, indent=2))
