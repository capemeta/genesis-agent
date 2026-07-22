---
name: skill-creator
description: Use this skill when the user wants to create, migrate, validate, package, or improve an agent skill for Genesis. Use for turning repeated workflows into SKILL.md packages, checking Claude/Codex/Kode skill compatibility, designing trigger descriptions, choosing references/scripts/assets, drafting eval prompts, or deciding that a request should be a document, script, tool, MCP, or plugin instead of a skill.
---

# Skill Creator

Help the user create small, routeable, maintainable Genesis Skills. Keep the first version useful before making it sophisticated.

## First Decide Whether A Skill Is Needed

Do not create a skill for one-off explanation, summary, translation, brainstorming, or documentation that does not need agent execution.

Prefer:

- A direct answer for one-off help.
- A document for reusable knowledge without execution flow.
- A script for deterministic single-purpose automation where routing is not the hard part.
- A Tool only for stable platform primitives.
- MCP for external systems, credentials, or SaaS actions.
- A Plugin when multiple Skills, MCP templates, assets, and installation lifecycle need one package.

Create a Skill when the work is recurring, routeable, benefits from reusable instructions or resources, and needs a clear input/output boundary.

## Intake

Ask only questions that change the package design:

1. What recurring job should the skill own?
2. What real inputs will users provide?
3. What output must the skill return?
4. What near-neighbor requests should not trigger it?
5. Which resources already exist: prompts, docs, scripts, examples, templates, or policies?
6. Does it need personal, team, or enterprise-governed treatment?

Stop asking when the skill can be summarized in one defensible sentence.

## Creation Workflow

1. Choose the smallest scope: `personal`, `team`, or `enterprise-governed`.
2. Write the `description` before expanding the body; it is the routing surface.
3. Put only trigger-critical guidance, core workflow, output contract, validation, and resource map in `SKILL.md`.
4. Move long domain guidance to `references/`.
5. Move deterministic, repetitive, fragile logic to `scripts/`.
6. Put templates, images, samples, and other output resources in `assets/`.
7. Generate only directories that are actually needed.
8. For team, marketplace, or enterprise-governed release, generate `skill-card.md` with `genesis-cli skill card generate <path>` and fill the governance fields.
9. Run `genesis-cli skill validate <path>` and, when a card exists or release is planned, `genesis-cli skill card validate <path>` before presenting the skill as ready.

## SKILL.md Template

```md
---
name: example-skill
description: Use this skill when the user needs ...
metadata:
  author: Genesis
---

# Example Skill

## Workflow

1. Understand the input and confirm missing constraints only when they affect the result.
2. Use referenced resources or scripts only when the task requires them.
3. Produce the output contract below.
4. Validate the result before responding.

## Output Contract

- ...

## Resource Map

- Read `references/example.md` when ...
- Use `scripts/example.ps1` when ...
```

## Resource Boundaries

- `SKILL.md` should stay lean and routeable.
- `references/` must be discoverable from `SKILL.md`.
- `scripts/` must be deterministic and reviewable.
- `assets/` are for output materials, not long instructions.
- `evals/` are optional for personal skills and recommended for team or enterprise-governed skills.
- `skill-card.md` is optional for personal drafts, recommended for team reuse, and should be required by marketplace or enterprise release policy.
- Empty decorative folders should be removed.

## Validation

Run:

```powershell
genesis-cli skill validate <path-to-skill>
genesis-cli skill card validate <path-to-skill>
```

Treat findings as:

- `error`: fix before loading or packaging.
- `warning`: review risk and decide whether policy or approval is needed.
- `info`: optional cleanup.

Check especially for missing resources, over-broad descriptions, scripts without declared risk, real secrets, host absolute paths, and unsupported platform assumptions.

## Migration Notes

When migrating Claude, Codex, Kode, or Agent Skills packages:

- Preserve the standard `SKILL.md`, `name`, `description`, `references/`, `scripts/`, and `assets/` shape when possible.
- Treat host-specific fields as extensions, not as Genesis authority.
- Replace Claude Code assumptions such as `claude -p`, `/tmp`, `nohup`, `open`, `present_files`, and Claude tool names with Genesis Run Engine, ToolGateway, Approval, Sandbox, and product UI.
- Do not copy source-available document skills into Genesis distribution unless the license allows redistribution.
- Keep Apache/MIT notices when reusing compatible external code.

## Genesis Runtime Boundary

Skill instructions never grant permission by themselves. Dependencies and allowed tools are declarations only.

Execution must still flow through:

```text
Skill declaration
  -> Skill validator / package scanner
  -> Capability catalog
  -> Policy filter
  -> Approval decision
  -> Sandbox / ToolGateway execution
  -> Audit / Usage record
```

For team, marketplace, or enterprise-governed skills, include owner, license/terms, deployment geography, requirements, risks/mitigations, references, output contract, version, ethical considerations, review cadence, source, scope, and rollback notes in `skill-card.md`, package metadata, or the publishing workflow.



