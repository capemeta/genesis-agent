"""Helpers for Genesis execution workspace paths.

各 office-* Skill 保持同名模块；签名需与 _office_common/scripts/office 兼容。
"""

import os


def logical_dir(name, fallback):
    return os.environ.get(name) or fallback


def input_dir():
    return logical_dir("INPUT_DIR", ".")


def output_dir():
    return logical_dir("OUTPUT_DIR", ".")


def work_dir():
    return logical_dir("WORK_DIR", ".")


def tmp_dir():
    return logical_dir("TMPDIR", os.environ.get("TEMP") or ".")


def resolve_input_path(raw):
    if os.path.isabs(raw) and os.path.exists(raw):
        return raw
    candidates = [raw, os.path.join(input_dir(), raw), os.path.join(input_dir(), os.path.basename(raw))]
    for candidate in candidates:
        if candidate and os.path.exists(candidate):
            return candidate
    return candidates[1] if len(candidates) > 1 else raw


def resolve_output_dir(raw=None):
    target = raw or output_dir()
    if not os.path.isabs(target):
        target = os.path.join(output_dir(), target)
    os.makedirs(target, exist_ok=True)
    return target


def resolve_output_path(raw, input_path=None):
    """解析最终交付文件路径（默认落在 OUTPUT_DIR）。

    input_path 可选：未传 raw 时用其 basename（兼容 office-excel recalc）。
    """
    if not raw:
        if input_path:
            raw = os.path.basename(input_path)
        else:
            raise ValueError("output path required")
    if os.path.isabs(raw):
        parent = os.path.dirname(raw)
        if parent:
            os.makedirs(parent, exist_ok=True)
        return raw
    target = os.path.join(output_dir(), raw)
    parent = os.path.dirname(target)
    if parent:
        os.makedirs(parent, exist_ok=True)
    return target


def resolve_work_path(raw):
    """解析工作目录/中间产物路径（默认落在 WORK_DIR）。"""
    if not raw:
        return work_dir()
    if os.path.isabs(raw):
        return raw
    if os.path.exists(raw):
        return raw
    return os.path.join(work_dir(), raw)
