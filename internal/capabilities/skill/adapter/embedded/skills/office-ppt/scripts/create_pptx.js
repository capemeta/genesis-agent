#!/usr/bin/env node
/**
 * Smoke-only pptxgenjs wrapper for Genesis office-ppt.
 *
 * NOT the default multi-page delivery path. For real decks:
 *   write a top-level pptxgenjs script → run_pptxgen_script.js
 *
 * Usage (CI / probe only):
 *   node create_pptx.js <output.pptx> [title] [subtitle]
 *   node create_pptx.js <output.pptx> --spec <deck.json>
 */

const fs = require("fs");
const path = require("path");

const SMOKE_WARNING =
  "create_pptx.js is smoke-only; for multi-page decks write a pptxgenjs script and run via office-ppt/scripts/run_pptxgen_script.js";

function outputDir() {
  return process.env.OUTPUT_DIR || ".";
}

function inputDir() {
  return process.env.INPUT_DIR || ".";
}

function resolveOutputPath(raw) {
  if (!raw) throw new Error("output path required");
  return path.isAbsolute(raw) ? raw : path.join(outputDir(), raw);
}

function resolveInputPath(raw) {
  if (!raw) throw new Error("input path required");
  return path.isAbsolute(raw) ? raw : path.join(inputDir(), raw);
}

function fail(message, extra) {
  const payload = Object.assign({ ok: false, errors: [message], warnings: [SMOKE_WARNING] }, extra || {});
  console.log(JSON.stringify(payload, null, 2));
  process.exit(1);
}

function parseArgs(argv) {
  if (argv.length < 3) {
    fail("usage: create_pptx.js <output.pptx> [title] [subtitle] OR create_pptx.js <output.pptx> --spec <deck.json>");
  }
  const out = { output: argv[2], title: argv[3] || "Presentation", subtitle: argv[4] || "", specPath: "" };
  for (let i = 3; i < argv.length; i++) {
    if (argv[i] === "--spec") {
      out.specPath = argv[i + 1] || "";
      i++;
    }
  }
  return out;
}

function loadSpec(args) {
  if (!args.specPath) {
    return {
      title: args.title,
      subtitle: args.subtitle,
      slides: [{ type: "title", title: args.title, subtitle: args.subtitle }],
    };
  }
  const specFile = resolveInputPath(args.specPath);
  const raw = fs.readFileSync(specFile, "utf8");
  const spec = JSON.parse(raw);
  if (!Array.isArray(spec.slides) || spec.slides.length === 0) {
    throw new Error("spec.slides must be a non-empty array");
  }
  return spec;
}

function asText(value) {
  if (value == null) return "";
  return String(value).replace(/\s+/g, " ").trim();
}

function cleanList(values) {
  if (!Array.isArray(values)) return [];
  return values.map(asText).filter(Boolean);
}

function clampText(text, max) {
  text = asText(text);
  return text.length > max ? text.slice(0, max - 1) + "…" : text;
}

function theme(spec) {
  const t = spec.theme || {};
  return {
    primary: normalizeColor(t.primary, "1E2761"),
    accent: normalizeColor(t.accent, "2B6CB0"),
    muted: normalizeColor(t.muted, "5B6472"),
    bg: normalizeColor(t.background, "F7F9FC"),
    line: normalizeColor(t.line, "D8DEE9"),
    text: normalizeColor(t.text, "1F2937"),
  };
}

function normalizeColor(value, fallback) {
  value = asText(value).replace(/^#/, "");
  return /^[0-9a-fA-F]{6}$/.test(value) ? value.toUpperCase() : fallback;
}

function addHeader(slide, title, t) {
  // Avoid title underline (Anthropic Avoid: accent lines under titles).
  slide.addShape("rect", { x: 0, y: 0, w: 13.333, h: 0.12, fill: { color: t.accent }, line: { color: t.accent } });
  slide.addText(clampText(title, 64), { x: 0.55, y: 0.32, w: 12.1, h: 0.42, fontSize: 19, bold: true, color: t.primary, margin: 0 });
}

function addFooter(slide, idx, total, t) {
  slide.addText(`${idx}/${total}`, { x: 12.05, y: 7.08, w: 0.7, h: 0.2, fontSize: 8, color: t.muted, align: "right", margin: 0 });
}

function addTitleSlide(pres, item, spec, idx, total, t) {
  const slide = pres.addSlide();
  slide.background = { color: "FFFFFF" };
  slide.addShape("rect", { x: 0, y: 0, w: 13.333, h: 7.5, fill: { color: t.bg }, line: { color: t.bg } });
  slide.addShape("rect", { x: 0, y: 0, w: 0.18, h: 7.5, fill: { color: t.accent }, line: { color: t.accent } });
  slide.addText(clampText(item.title || spec.title || "Presentation", 54), { x: 0.9, y: 2.35, w: 11.4, h: 0.7, fontSize: 31, bold: true, color: t.primary, align: "center", margin: 0 });
  const subtitle = item.subtitle || spec.subtitle || "";
  if (subtitle) {
    slide.addText(clampText(subtitle, 90), { x: 1.5, y: 3.25, w: 10.2, h: 0.38, fontSize: 15, color: t.text, align: "center", margin: 0 });
  }
  addFooter(slide, idx, total, t);
}

function addBulletsSlide(pres, item, idx, total, t) {
  const slide = pres.addSlide();
  slide.background = { color: "FFFFFF" };
  addHeader(slide, item.title || "Key Points", t);
  const bullets = cleanList(item.bullets || item.points).slice(0, 8);
  const rows = bullets.map((b) => ({ text: clampText(b, 110), options: { bullet: { indent: 16 }, hanging: 4, breakLine: true } }));
  slide.addText(rows.length ? rows : [{ text: " ", options: {} }], { x: 0.9, y: 1.25, w: 11.5, h: 5.35, fontSize: bullets.length > 6 ? 14 : 16, color: t.text, fit: "shrink", valign: "top", paraSpaceAfterPt: 8, margin: 0.05, breakLine: false });
  addFooter(slide, idx, total, t);
}

function addTableSlide(pres, item, idx, total, t) {
  const slide = pres.addSlide();
  slide.background = { color: "FFFFFF" };
  addHeader(slide, item.title || "Comparison", t);
  const table = item.table || {};
  const headers = cleanList(table.headers).slice(0, 6);
  const rows = Array.isArray(table.rows) ? table.rows.slice(0, 8) : [];
  const data = [];
  if (headers.length > 0) data.push(headers.map((h) => ({ text: clampText(h, 28), options: { bold: true, color: "FFFFFF", fill: { color: t.primary } } })));
  for (const row of rows) {
    const cells = Array.isArray(row) ? row : [];
    data.push(headers.map((_, i) => ({ text: clampText(cells[i] || "", 44), options: { color: t.text } })));
  }
  if (data.length === 0) {
    addBulletsSlide(pres, { title: item.title, bullets: cleanList(item.bullets) }, idx, total, t);
    return;
  }
  slide.addTable(data, {
    x: 0.45,
    y: 1.2,
    w: 12.45,
    h: 5.55,
    fontSize: rows.length > 5 ? 8.5 : 9.5,
    border: { type: "solid", color: t.line, pt: 0.6 },
    margin: 0.06,
    valign: "mid",
    fit: "shrink",
    color: t.text,
  });
  addFooter(slide, idx, total, t);
}

function addTwoColumnSlide(pres, item, idx, total, t) {
  const slide = pres.addSlide();
  slide.background = { color: "FFFFFF" };
  addHeader(slide, item.title || "Recommendations", t);
  const columns = Array.isArray(item.columns) ? item.columns.slice(0, 2) : [];
  const width = 5.8;
  columns.forEach((col, i) => {
    const x = 0.75 + i * 6.15;
    slide.addShape("roundRect", { x, y: 1.25, w: width, h: 5.35, rectRadius: 0.08, fill: { color: i === 0 ? "EEF4FF" : "F8FAFC" }, line: { color: t.line, width: 1 } });
    slide.addText(clampText(col.heading || col.title || `Column ${i + 1}`, 36), { x: x + 0.28, y: 1.55, w: width - 0.56, h: 0.32, fontSize: 15, bold: true, color: t.primary, margin: 0 });
    const bullets = cleanList(col.bullets || col.points).slice(0, 6);
    const text = bullets.map((b) => ({ text: clampText(b, 92), options: { bullet: { indent: 14 }, hanging: 3, breakLine: true } }));
    slide.addText(text, { x: x + 0.35, y: 2.05, w: width - 0.7, h: 4.05, fontSize: bullets.length > 4 ? 11.5 : 12.5, color: t.text, fit: "shrink", valign: "top", paraSpaceAfterPt: 5, margin: 0.02 });
  });
  addFooter(slide, idx, total, t);
}

function addSlide(pres, item, spec, idx, total, t) {
  const type = asText(item.type || item.layout || "bullets").toLowerCase();
  if (type === "title" || idx === 1 && !item.bullets && !item.table && !item.columns) return addTitleSlide(pres, item, spec, idx, total, t);
  if (type === "table" || item.table) return addTableSlide(pres, item, idx, total, t);
  if (type === "two_column" || type === "two-column" || item.columns) return addTwoColumnSlide(pres, item, idx, total, t);
  return addBulletsSlide(pres, item, idx, total, t);
}

async function main(argv) {
  const args = parseArgs(argv);

  let pptxgen;
  try {
    pptxgen = require("pptxgenjs");
  } catch (err) {
    fail("pptxgenjs not installed; run: npm install pptxgenjs (office-basic or Node env)", { hint: "dependency_missing", dependency: "pptxgenjs" });
  }

  const outPath = resolveOutputPath(args.output);
  const spec = loadSpec(args);
  const pres = new pptxgen();
  // Smoke layout; real decks should pick layout in user pptxgen scripts.
  pres.layout = "LAYOUT_WIDE";
  pres.title = spec.title || args.title || "Presentation";
  pres.subject = spec.subtitle || args.subtitle || "";
  pres.author = spec.author || "Genesis Agent";
  pres.company = "Genesis Agent";
  pres.lang = spec.lang || "zh-CN";
  pres.theme = {
    headFontFace: "Microsoft YaHei",
    bodyFontFace: "Microsoft YaHei",
    lang: "zh-CN",
  };

  const t = theme(spec);
  const slides = spec.slides || [];
  slides.forEach((slide, i) => addSlide(pres, slide || {}, spec, i + 1, slides.length, t));

  const parent = path.dirname(outPath);
  fs.mkdirSync(parent, { recursive: true });
  await pres.writeFile({ fileName: outPath });

  const stat = fs.statSync(outPath);
  console.log(JSON.stringify({
    ok: true,
    path: outPath,
    size_bytes: stat.size,
    slides: slides.length,
    artifacts: [{ path: outPath, kind: "pptx" }],
    warnings: [SMOKE_WARNING],
  }, null, 2));
}

main(process.argv).catch((err) => {
  fail(String(err && err.message ? err.message : err));
});
