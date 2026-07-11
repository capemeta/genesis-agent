#!/usr/bin/env node
/**
 * Anthropic-aligned pptxgen runner for Genesis office-ppt.
 *
 * Agent writes a top-level pptxgenjs script (same style as references/pptxgenjs.md),
 * stages it via inputs, then this fixed ResourceID spawns a fresh node process:
 *
 *   run_skill_script(
 *     script="office-ppt/scripts/run_pptxgen_script.js",
 *     args=["deck_gen.js"],
 *     inputs=["$WORK_DIR/deck_gen.js"])
 *
 * User script must be written under $WORK_DIR (not repo root), then staged via inputs.
 * User script must write the .pptx under process.env.OUTPUT_DIR.
 * Do not use create_pptx.js for multi-page delivery.
 */

const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

function fail(message, extra) {
  const payload = Object.assign({ ok: false, errors: [message] }, extra || {});
  console.log(JSON.stringify(payload, null, 2));
  process.exit(1);
}

function main(argv) {
  const raw = argv[2];
  if (!raw || String(raw).trim() === "") {
    fail("usage: run_pptxgen_script.js <user_script.js>");
  }

  const inputDir = process.env.INPUT_DIR || ".";
  const scriptName = path.basename(String(raw).trim());
  const scriptPath = path.join(inputDir, scriptName);
  if (!fs.existsSync(scriptPath)) {
    fail("user script not found in INPUT_DIR: " + scriptName, {
      input_dir: inputDir,
      hint: "stage the .js via inputs and pass only the filename in args",
    });
  }

  try {
    require.resolve("pptxgenjs");
  } catch (err) {
    fail("pptxgenjs not installed; run: npm install pptxgenjs (office-basic or Node env)", {
      hint: "dependency_missing",
      dependency: "pptxgenjs",
    });
  }

  // Fresh process each run — avoids require() cache when Agent edits the script.
  const result = spawnSync(process.execPath, [scriptPath], {
    env: process.env,
    encoding: "utf8",
    cwd: inputDir,
  });

  if (result.stdout) {
    process.stdout.write(result.stdout);
  }
  if (result.stderr) {
    process.stderr.write(result.stderr);
  }
  if (result.error) {
    fail(String(result.error.message || result.error));
  }
  const code = result.status == null ? 1 : result.status;
  if (code !== 0) {
    process.exit(code);
  }
}

main(process.argv);
