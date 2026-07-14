#!/usr/bin/env python3
"""register_cjk_font 回退逻辑单测（位于技能包外，避免 go:embed）。"""

from __future__ import annotations

import importlib.util
import sys
import unittest
from pathlib import Path
from unittest import mock

_SCRIPT = (
    Path(__file__).resolve().parents[0]
    / "skills"
    / "office-pdf"
    / "scripts"
    / "register_cjk_font.py"
)


def _load_module():
    spec = importlib.util.spec_from_file_location("register_cjk_font", _SCRIPT)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class EnsureReportlabCJKFallbackTest(unittest.TestCase):
    def setUp(self) -> None:
        self.mod = _load_module()

    def test_skips_unregistrable_font_and_uses_next(self) -> None:
        bad = Path("/fake/NotoSansCJK-Regular.ttc")
        good = Path("/fake/wqy-zenhei.ttc")
        ps_err = Exception(
            'TTC file "/fake/NotoSansCJK-Regular.ttc": postscript outlines are not supported'
        )

        def fake_ttfont(name, path, subfontIndex=0):  # noqa: N803
            if "Noto" in path:
                raise ps_err
            return mock.Mock(name=f"TTFont({path})")

        with (
            mock.patch.object(self.mod, "iter_existing_cjk_fonts", return_value=[bad, good]),
            mock.patch("reportlab.pdfbase.pdfmetrics.getRegisteredFontNames", return_value=[]),
            mock.patch("reportlab.pdfbase.pdfmetrics.registerFont") as register_font,
            mock.patch("reportlab.pdfbase.ttfonts.TTFont", side_effect=fake_ttfont),
        ):
            name = self.mod.ensure_reportlab_cjk("CJK")
            self.assertEqual(name, "CJK")
            register_font.assert_called_once()
            call_arg = register_font.call_args[0][0]
            self.assertIn("wqy-zenhei", str(call_arg))

    def test_raises_when_all_candidates_fail(self) -> None:
        only = Path("/fake/NotoSansCJK-Regular.ttc")

        with (
            mock.patch.object(self.mod, "iter_existing_cjk_fonts", return_value=[only]),
            mock.patch("reportlab.pdfbase.pdfmetrics.getRegisteredFontNames", return_value=[]),
            mock.patch(
                "reportlab.pdfbase.ttfonts.TTFont",
                side_effect=Exception("postscript outlines are not supported"),
            ),
            mock.patch("reportlab.pdfbase.pdfmetrics.registerFont"),
        ):
            with self.assertRaises(RuntimeError) as ctx:
                self.mod.ensure_reportlab_cjk("CJK")
            self.assertIn("postscript outlines", str(ctx.exception))

    def test_register_reportlab_cjk_returns_winning_path(self) -> None:
        bad = Path("/fake/NotoSansCJK-Regular.ttc")
        good = Path("/fake/wqy-zenhei.ttc")

        def fake_ttfont(name, path, subfontIndex=0):  # noqa: N803
            if "Noto" in path:
                raise Exception("postscript outlines are not supported")
            return mock.Mock()

        with (
            mock.patch.object(self.mod, "iter_existing_cjk_fonts", return_value=[bad, good]),
            mock.patch("reportlab.pdfbase.pdfmetrics.getRegisteredFontNames", return_value=[]),
            mock.patch("reportlab.pdfbase.pdfmetrics.registerFont"),
            mock.patch("reportlab.pdfbase.ttfonts.TTFont", side_effect=fake_ttfont),
        ):
            path, name = self.mod.register_reportlab_cjk("CJK")
            self.assertEqual(path, good)
            self.assertEqual(name, "CJK")


if __name__ == "__main__":
    # RED: 当前实现尚无 iter_existing_cjk_fonts / register_reportlab_cjk
    unittest.main()
