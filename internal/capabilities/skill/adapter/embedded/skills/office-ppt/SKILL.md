---
name: office-ppt
description: "Use this skill any time a .pptx file is involved in any way — as input, output, or both. This includes: creating slide decks, pitch decks, or presentations; reading, parsing, or extracting text from any .pptx file (even if the extracted content will be used elsewhere, like in an email or summary); editing, modifying, or updating existing presentations; combining or splitting slide files; working with templates, layouts, speaker notes, or comments. Trigger whenever the user mentions deck, slides, presentation, or references a .pptx filename, regardless of what they plan to do with the content afterward. If a .pptx file needs to be opened, created, or touched, use this skill."
license: Proprietary. LICENSE.txt has complete terms
allowed-tools:
  - Skill
  - list_skill_resources
  - read_skill_resource
  - search_skill_resources
  - run_skill_command
  - install_skill_dependencies
  - read_file
  - write_file
  - edit_file
dependencies:
  runtime:
    python:
      - name: markitdown
        import: markitdown
      - name: Pillow
        import: PIL
      - name: defusedxml
        import: defusedxml
    node:
      - name: pptxgenjs
        require: pptxgenjs
    system:
      - name: libreoffice
        command: soffice
      - name: poppler
        command: pdftoppm
---



# PPTX Skill



## Quick Reference


| Task                         | Guide                                                                             |
| ---------------------------- | --------------------------------------------------------------------------------- |
| Read/analyze content         | `python -m markitdown presentation.pptx`                                          |
| Edit or create from template | Read [editing.md](editing.md)                                                     |
| Create from scratch          | Write a Node `.js` using [pptxgenjs.md](pptxgenjs.md), then `node your_script.js` |


---



## Reading Content

```bash
# Text extraction
python -m markitdown presentation.pptx

# Visual overview
python scripts/thumbnail.py presentation.pptx

# Raw XML
python scripts/office/unpack.py presentation.pptx unpacked/
```

---



## Editing Workflow

**Read [editing.md](editing.md) for full details.**

1. Analyze template with `thumbnail.py`
2. Unpack → manipulate slides → edit content → clean → pack

---



## Creating from Scratch

**Mandatory order — step by step ,do not skip:**

1. **Read [pptxgenjs.md](pptxgenjs.md) completely** before writing any creation script (especially Common Pitfalls: `lineSpacing` is points, multi-line needs `breakLine`).
2. **Read [design.md](design.md)** and pick one palette + one visual motif; apply them in the script (do not ship plain white slides with default gray tables).
3. Write a **Node.js** script that `require("pptxgenjs")` (not Python). Default creation depends only on declared runtime (`pptxgenjs`); use shapes for icons — see [pptxgenjs.md](pptxgenjs.md).
4. Run it with `node your_script.js` (write the script to a file first; avoid long `node -e` one-liners).
5. **Content QA:** `python -m markitdown your.pptx` and confirm text was extracted.
6. **Visual QA (required):** `python scripts/thumbnail.py your.pptx` — open the thumbnail and check for overlapping text, cramped rows, and low contrast. `markitdown` cannot catch layout bugs.

Use when no template or reference presentation is available.

Do **not** run `python -m pptxgenjs`. `addSlide()` returns a slide object — add content with `slide.addText` / `slide.addTable` / `slide.addShape`. Do not pass `title` / `table` / `bullet` / `layout` into `addSlide(...)` (that yields blank slides).

Never use `lineSpacing: 1.2` / `1.4` / `1.5` as if it were CSS line-height — that collapses text. Use `breakLine` arrays (see [pptxgenjs.md](pptxgenjs.md)).

---



## Design Ideas

**Required for create-from-scratch.** Read [design.md](design.md) for palettes, typography, layout motifs, and common visual mistakes before writing the creation script.

---



## QA (Required)

**Assume there are problems. Your job is to find them.**

Your first render is almost never correct. Approach QA as a bug hunt, not a confirmation step. If you found zero issues on first inspection, you weren't looking hard enough.

### Content QA

```bash
python -m markitdown output.pptx
```

Check for missing content, typos, wrong order.

**When using templates, check for leftover placeholder text:**

```bash
python -m markitdown output.pptx | grep -iE "xxxx|lorem|ipsum|this.*(page|slide).*layout"
```

If grep returns results, fix them before declaring success.

### Visual QA

**Required after every create/edit.** `markitdown` only checks text — it will **not** catch overlapping lines, stacked text, or bad spacing.

```bash
python scripts/thumbnail.py output.pptx
```

Open the generated thumbnail and look for overlapping text, cut-off content, and low contrast. Fix and re-run before finishing.

**⚠️ USE SUBAGENTS** when available — even for 2-3 slides. You've been staring at the code and will see what you expect, not what's there. Subagents have fresh eyes.

For deeper inspection, convert slides to images (see [Converting to Images](#converting-to-images)), then use this prompt:

```
Visually inspect these slides. Assume there are issues — find them.

Look for:
- Overlapping elements (text through shapes, lines through words, stacked elements)
- Text overflow or cut off at edges/box boundaries
- Decorative lines positioned for single-line text but title wrapped to two lines
- Source citations or footers colliding with content above
- Elements too close (< 0.3" gaps) or cards/sections nearly touching
- Uneven gaps (large empty area in one place, cramped in another)
- Insufficient margin from slide edges (< 0.5")
- Columns or similar elements not aligned consistently
- Low-contrast text (e.g., light gray text on cream-colored background)
- Low-contrast icons (e.g., dark icons on dark backgrounds without a contrasting circle)
- Text boxes too narrow causing excessive wrapping
- Leftover placeholder content

For each slide, list issues or areas of concern, even if minor.

Read and analyze these images:
1. /path/to/slide-01.jpg (Expected: [brief description])
2. /path/to/slide-02.jpg (Expected: [brief description])

Report ALL issues found, including minor ones.
```



### Verification Loop

1. Generate slides → Convert to images → Inspect
2. **List issues found** (if none found, look again more critically)
3. Fix issues
4. **Re-verify affected slides** — one fix often creates another problem
5. Repeat until a full pass reveals no new issues

**Do not declare success until you've completed at least one fix-and-verify cycle.**

---



## Converting to Images

Convert presentations to individual slide images for visual inspection:

```bash
python scripts/office/soffice.py --headless --convert-to pdf output.pptx
pdftoppm -jpeg -r 150 output.pdf slide
```

This creates `slide-01.jpg`, `slide-02.jpg`, etc.

To re-render specific slides after fixes:

```bash
pdftoppm -jpeg -r 150 -f N -l N output.pdf slide-fixed
```

---



## Dependencies

- `pip install "markitdown[pptx]"` - text extraction
- `pip install Pillow` - thumbnail grids
- `npm install -g pptxgenjs` - creating from scratch
- LibreOffice (`soffice`) - PDF conversion (auto-configured for sandboxed environments via `scripts/office/soffice.py`)
- Poppler (`pdftoppm`) - PDF to images