#!/usr/bin/env node
/**
 * Minimal pptxgenjs wrapper for Genesis office-ppt.
 *
 * Usage:
 *   node create_pptx.js <output.pptx> [title] [subtitle]
 *
 * Writes a real OOXML .pptx into OUTPUT_DIR (via path resolution).
 * Requires: npm install pptxgenjs (in sandbox image or local env).
 */

const fs = require("fs");
const path = require("path");

function outputDir() {
  return process.env.OUTPUT_DIR || ".";
}

function resolveOutputPath(raw) {
  if (!raw) {
    throw new Error("output path required");
  }
  if (path.isAbsolute(raw)) {
    return raw;
  }
  return path.join(outputDir(), raw);
}

function fail(message, extra) {
  const payload = Object.assign({ ok: false, errors: [message] }, extra || {});
  console.log(JSON.stringify(payload, null, 2));
  process.exit(1);
}

async function main(argv) {
  if (argv.length < 3) {
    fail("usage: create_pptx.js <output.pptx> [title] [subtitle]");
  }

  let pptxgen;
  try {
    pptxgen = require("pptxgenjs");
  } catch (err) {
    fail(
      "pptxgenjs not installed; run: npm install pptxgenjs (office-basic / Node env)",
      { hint: "dependency_missing", dependency: "pptxgenjs" }
    );
  }

  const outPath = resolveOutputPath(argv[2]);
  const title = argv[3] || "Presentation";
  const subtitle = argv[4] || "";

  const pres = new pptxgen();
  pres.layout = "LAYOUT_16x9";
  pres.title = title;
  pres.author = "Genesis Agent";

  const slide = pres.addSlide();
  slide.addText(title, {
    x: 0.5,
    y: 2.0,
    w: 9.0,
    h: 1.0,
    fontSize: 40,
    bold: true,
    color: "1E2761",
    align: "center",
  });
  if (subtitle) {
    slide.addText(subtitle, {
      x: 0.5,
      y: 3.1,
      w: 9.0,
      h: 0.6,
      fontSize: 18,
      color: "36454F",
      align: "center",
    });
  }

  const parent = path.dirname(outPath);
  fs.mkdirSync(parent, { recursive: true });
  await pres.writeFile({ fileName: outPath });

  const stat = fs.statSync(outPath);
  console.log(
    JSON.stringify(
      {
        ok: true,
        path: outPath,
        size_bytes: stat.size,
        slides: 1,
        artifacts: [{ path: outPath, kind: "pptx" }],
        warnings: [],
      },
      null,
      2
    )
  );
}

main(process.argv).catch((err) => {
  fail(String(err && err.message ? err.message : err));
});
