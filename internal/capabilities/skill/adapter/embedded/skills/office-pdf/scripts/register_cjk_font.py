#!/usr/bin/env python3
"""跨平台探测可用 CJK 字体路径；可选注册为 reportlab 字体名 CJK。

不捆绑字体文件：优先系统/镜像常见路径（含 office-basic 的 fonts-noto-cjk）。
可选探测技能包内 fonts/（若运维另行放置）。

注册时会对每个候选尝试 reportlab TTFont；若遇
「postscript outlines are not supported」等失败则自动跳过并尝试下一个
（office-basic 中 Noto CJK *.ttc 常为 CFF，需回退到 wqy-zenhei 等）。

用法:
  python scripts/register_cjk_font.py
  python scripts/register_cjk_font.py --register

其它脚本:
  from register_cjk_font import find_cjk_font, ensure_reportlab_cjk
  font_name = ensure_reportlab_cjk()  # -> "CJK"
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import sys
from pathlib import Path


def _skill_root() -> Path:
    # scripts/register_cjk_font.py -> 技能根目录
    return Path(__file__).resolve().parent.parent


def candidate_font_paths() -> list[Path]:
    """按优先级返回候选字体路径（存在性由 iter_existing_cjk_fonts 检查）。"""
    root = _skill_root()
    candidates: list[Path] = []

    # 1) 技能包可选 fonts/（默认不随包分发；支持运维外置）
    fonts_dir = root / "fonts"
    if fonts_dir.is_dir():
        for pattern in (
            "NotoSansSC-Regular.otf",
            "NotoSansSC-Regular.ttf",
            "NotoSansCJK-Regular.ttc",
            "NotoSansCJKsc-Regular.otf",
            "*.otf",
            "*.ttf",
            "*.ttc",
        ):
            candidates.extend(sorted(fonts_dir.glob(pattern)))

    system = platform.system()
    if system == "Windows":
        windir = Path(os.environ.get("WINDIR", r"C:\Windows"))
        fontdir = windir / "Fonts"
        candidates.extend(
            [
                fontdir / "msyh.ttc",  # 微软雅黑
                fontdir / "msyhbd.ttc",
                fontdir / "msyhl.ttc",
                fontdir / "simhei.ttf",  # 黑体
                fontdir / "simsun.ttc",  # 宋体
                fontdir / "simkai.ttf",
                fontdir / "simfang.ttf",
                fontdir / "NotoSansSC-Regular.otf",
                fontdir / "NotoSansCJK-Regular.ttc",
            ]
        )
    elif system == "Darwin":
        candidates.extend(
            [
                Path("/System/Library/Fonts/PingFang.ttc"),
                Path("/System/Library/Fonts/STHeiti Light.ttc"),
                Path("/System/Library/Fonts/STHeiti Medium.ttc"),
                Path("/System/Library/Fonts/Hiragino Sans GB.ttc"),
                Path("/Library/Fonts/Arial Unicode.ttf"),
                Path("/System/Library/Fonts/Supplemental/Arial Unicode.ttf"),
                Path("/Library/Fonts/NotoSansSC-Regular.otf"),
                Path("/Library/Fonts/NotoSansCJK-Regular.ttc"),
            ]
        )
    else:
        # Linux / 沙箱 office-basic：
        # 优先 TrueType 友好字体（reportlab TTFont 可注册）；
        # Noto CJK *.ttc 多为 CFF/PostScript outlines，放后面作探测备选。
        candidates.extend(
            [
                Path("/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"),
                Path("/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"),
                Path("/usr/share/fonts/truetype/arphic/uming.ttc"),
                Path("/usr/share/fonts/truetype/arphic/ukai.ttc"),
                Path("/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"),
                Path("/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf"),
                Path("/usr/share/fonts/truetype/noto/NotoSansCJKsc-Regular.otf"),
                Path("/usr/share/fonts/opentype/noto/NotoSansSC-Regular.otf"),
                Path("/usr/share/fonts/truetype/noto/NotoSansSC-Regular.otf"),
                Path("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"),
                Path("/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc"),
            ]
        )

    # 去重并保序
    seen: set[str] = set()
    unique: list[Path] = []
    for path in candidates:
        key = str(path)
        if key in seen:
            continue
        seen.add(key)
        unique.append(path)
    return unique


def iter_existing_cjk_fonts() -> list[Path]:
    """返回磁盘上存在的候选字体（保序）。"""
    found: list[Path] = []
    for path in candidate_font_paths():
        try:
            if path.is_file() and path.stat().st_size > 0:
                found.append(path)
        except OSError:
            continue
    return found


def find_cjk_font() -> Path | None:
    fonts = iter_existing_cjk_fonts()
    return fonts[0] if fonts else None


def register_reportlab_cjk(font_name: str = "CJK") -> tuple[Path, str]:
    """尝试注册 reportlab 可用的 CJK 字体，返回 (成功路径, fontName)。

    某个候选注册失败时跳过并试下一个；全部失败则抛 RuntimeError。
    """
    fonts = iter_existing_cjk_fonts()
    if not fonts:
        raise RuntimeError(
            "未找到可用 CJK 字体。请安装 fonts-noto-cjk（Linux/沙箱）、"
            "微软雅黑/黑体（Windows）或 PingFang（macOS）；"
            "也可将开源字体放到技能 fonts/ 目录。"
        )
    try:
        from reportlab.pdfbase import pdfmetrics
        from reportlab.pdfbase.ttfonts import TTFont
    except ImportError as exc:
        raise RuntimeError("需要 reportlab 才能注册 PDF 字体") from exc

    registered = set(pdfmetrics.getRegisteredFontNames())
    if font_name in registered:
        # 已注册则沿用；路径仍返回第一个存在的候选供日志参考
        return fonts[0], font_name

    errors: list[str] = []
    for path in fonts:
        try:
            # TTC 集合字体用 subfontIndex=0；单字形 TTF/OTF 同样可传 0
            pdfmetrics.registerFont(TTFont(font_name, str(path), subfontIndex=0))
            return path, font_name
        except Exception as exc:  # noqa: BLE001 - 需汇总各候选失败原因
            errors.append(f"{path}: {exc}")
            continue

    detail = "; ".join(errors)
    raise RuntimeError(
        "找到 CJK 字体文件但均无法被 reportlab 注册"
        f"（常见原因：Noto CJK TTC 为 PostScript/CFF outlines）。尝试记录: {detail}"
    )


def ensure_reportlab_cjk(font_name: str = "CJK") -> str:
    """注册 reportlab 可用的 CJK 字体，返回 fontName。失败抛 RuntimeError。"""
    _, name = register_reportlab_cjk(font_name)
    return name


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="探测并可选注册 CJK 字体")
    parser.add_argument(
        "--register",
        action="store_true",
        help="向 reportlab 注册字体名 CJK（需已安装 reportlab）",
    )
    parser.add_argument(
        "--name",
        default="CJK",
        help="reportlab 字体注册名（默认 CJK）",
    )
    args = parser.parse_args(argv)

    path = find_cjk_font()
    payload = {
        "ok": path is not None,
        "path": str(path) if path else None,
        "platform": platform.system(),
        "font_name": None,
        "error": None,
    }
    if path is None:
        payload["error"] = (
            "no CJK font found in skill fonts/, system Fonts, or common Linux/Noto paths"
        )
        print(json.dumps(payload, ensure_ascii=False))
        return 1

    if args.register:
        try:
            used_path, name = register_reportlab_cjk(args.name)
            payload["path"] = str(used_path)
            payload["font_name"] = name
        except Exception as exc:  # noqa: BLE001 - CLI 需把原因回传 JSON
            payload["ok"] = False
            payload["error"] = str(exc)
            print(json.dumps(payload, ensure_ascii=False))
            return 2
    else:
        payload["font_name"] = args.name

    print(json.dumps(payload, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
