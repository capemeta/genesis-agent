#!/usr/bin/env python3
"""从 PPTX 包中抽取幻灯片文本、演讲者备注和评论。"""

import argparse
import json
import os
import re
import sys
import zipfile
import xml.etree.ElementTree as ET

from path_contract import configure_utf8_stdio, emit_json, resolve_input_path

SLIDE_RE = re.compile(r"ppt/slides/slide(\d+)\.xml$")
REL_NS = {"r": "http://schemas.openxmlformats.org/package/2006/relationships"}
A_TEXT = "{http://schemas.openxmlformats.org/drawingml/2006/main}t"
NOTES_REL_TYPE = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide"


def local_name(tag):
    return tag.rsplit("}", 1)[-1] if "}" in tag else tag


def read_xml(zf, name):
    return ET.fromstring(zf.read(name))


def drawing_text(root):
    values = []
    for elem in root.iter():
        if elem.tag == A_TEXT and elem.text:
            text = elem.text.strip()
            if text:
                values.append(text)
    return values


def generic_text(root):
    values = []
    for elem in root.iter():
        if elem.text and local_name(elem.tag) not in {"sld", "cSld", "spTree"}:
            text = elem.text.strip()
            if text:
                values.append(text)
    return values


def slide_number(name):
    match = SLIDE_RE.match(name)
    return int(match.group(1)) if match else 0


def normalize_target(base_dir, target):
    joined = os.path.normpath(os.path.join(base_dir, target)).replace("\\", "/")
    return joined.lstrip("/")


def notes_for_slide(zf, slide_name):
    rel_name = slide_name.replace("ppt/slides/", "ppt/slides/_rels/") + ".rels"
    if rel_name not in zf.namelist():
        return None
    rels = read_xml(zf, rel_name)
    for rel in rels.findall("r:Relationship", REL_NS):
        if rel.attrib.get("Type") == NOTES_REL_TYPE:
            return normalize_target("ppt/slides", rel.attrib.get("Target", ""))
    return None


def extract(path):
    with zipfile.ZipFile(path) as zf:
        names = set(zf.namelist())
        slides = sorted((name for name in names if SLIDE_RE.match(name)), key=slide_number)
        result_slides = []
        all_notes = set()

        for slide_name in slides:
            slide_root = read_xml(zf, slide_name)
            notes_name = notes_for_slide(zf, slide_name)
            notes_text = []
            if notes_name and notes_name in names:
                all_notes.add(notes_name)
                notes_text = drawing_text(read_xml(zf, notes_name))
            result_slides.append(
                {
                    "number": slide_number(slide_name),
                    "path": slide_name,
                    "text": drawing_text(slide_root),
                    "notes_path": notes_name or "",
                    "notes": notes_text,
                }
            )

        orphan_notes = []
        for name in sorted(n for n in names if n.startswith("ppt/notesSlides/") and n.endswith(".xml")):
            if name not in all_notes:
                orphan_notes.append({"path": name, "text": drawing_text(read_xml(zf, name))})

        comments = []
        for name in sorted(n for n in names if n.startswith("ppt/comments/") and n.endswith(".xml")):
            comments.append({"path": name, "text": generic_text(read_xml(zf, name))})

        return {
            "ok": True,
            "path": path,
            "slides": result_slides,
            "orphan_notes": orphan_notes,
            "comments": comments,
            "warnings": [],
        }


def to_markdown(data, include_empty):
    lines = [f"# {os.path.basename(data['path'])}", ""]
    for slide in data["slides"]:
        text = slide["text"]
        notes = slide["notes"]
        if not include_empty and not text and not notes:
            continue
        lines.append(f"## Slide {slide['number']}")
        if text:
            lines.extend(f"- {item}" for item in text)
        elif include_empty:
            lines.append("- [no extractable slide text]")
        if notes:
            lines.append("")
            lines.append("Speaker notes:")
            lines.extend(f"- {item}" for item in notes)
        lines.append("")
    if data["comments"]:
        lines.append("## Comments")
        for comment in data["comments"]:
            if comment["text"] or include_empty:
                lines.append(f"### {comment['path']}")
                lines.extend(f"- {item}" for item in comment["text"])
                lines.append("")
    if data["orphan_notes"]:
        lines.append("## Orphan Notes")
        for note in data["orphan_notes"]:
            if note["text"] or include_empty:
                lines.append(f"### {note['path']}")
                lines.extend(f"- {item}" for item in note["text"])
                lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def main(argv):
    parser = argparse.ArgumentParser(description="从 PPTX 文件抽取可读文本。")
    parser.add_argument("input", help="已 stage 到 INPUT_DIR 的 .pptx 文件名，或绝对路径。")
    parser.add_argument("--format", choices=["json", "markdown"], default="json")
    parser.add_argument("--include-empty", action="store_true")
    args = parser.parse_args(argv[1:])

    path = resolve_input_path(args.input)
    if not os.path.exists(path):
        emit_json({"ok": False, "errors": [f"file not found: {path}"]}, exit_code=1)
        return 1
    try:
        data = extract(path)
    except Exception as exc:
        emit_json({"ok": False, "path": path, "errors": [str(exc)]}, exit_code=1)
        return 1

    if args.format == "markdown":
        configure_utf8_stdio()
        try:
            print(to_markdown(data, args.include_empty), end="")
        except UnicodeEncodeError:
            # markdown 含 emoji 时回退到 JSON，避免 Windows GBK 崩溃。
            emit_json(data)
        return 0
    emit_json(data)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

