---
name: code-doc-alignment
description: Audit alignment between reference documents and actual implementation. Map requirements, designs, specs, plans, or decisions to code and classify each item as implemented, partially implemented, or unimplemented. Produce an evidence-based comparison table, gap analysis, and actionable next steps. Language follows the user's request and project instructions.
---

# Code-Doc Alignment

This skill provides a systematic workflow for auditing whether implementation matches its reference sources, such as design documents, specifications, architecture decisions, roadmaps, issue descriptions, task plans, or prior agreed context.

Use it to clarify implementation progress, expose design drift, identify undocumented implementation choices, and produce a practical next-step plan.

## Trigger Phrases

- `$code-doc-alignment`
- `code-doc alignment`
- `align docs and code`
- `audit code against design/spec`
- `generate implementation progress table`
- `对齐文档与代码`
- `生成实现进度对照表`
- `审计代码与设计文档一致性`
- `分析已实现未实现部分并做反思`
- `代码与文档对齐审计`

## Core Goals

1. **Scope alignment**: Identify the reference sources and the implementation areas that should be compared.
2. **Evidence mapping**: Map each requirement, design point, contract, workflow, or user-visible behavior to concrete files, modules, tests, configuration, or absence of implementation.
3. **Status classification**: Classify each item as implemented, partially implemented, or unimplemented using explicit evidence.
4. **Gap analysis**: Explain implementation drift, missing behavior, outdated documentation, ambiguous requirements, or deliberate design changes.
5. **Actionable next steps**: Propose prioritized follow-up work with concrete validation criteria.
6. **Optional closure loop**: If explicitly requested, use a review-fix-rereview workflow to refine the audit until no material inaccuracies remain.

## Workflow

### 1. Define The Audit Scope

- **Identify reference sources**: Locate the relevant design docs, specs, ADRs, planning docs, issue text, task descriptions, changelogs, or prior agreed context.
- **Identify implementation targets**: Locate the packages, modules, files, tests, schemas, configs, APIs, UI flows, or runtime behavior that represent the implementation.
- **Identify project constraints**: Bring in repository-level instructions, architecture boundaries, language conventions, product/module isolation rules, security constraints, data rules, and validation expectations from project instructions and existing patterns.
- **Clarify ambiguity only when needed**: If the audit cannot be scoped safely from local context, ask the smallest necessary question. Otherwise, make a reasonable assumption and state it.

### 2. Map Requirements To Implementation

For each meaningful reference item, inspect the implementation and classify it as:

- **`[x] Implemented`**: The core behavior exists, is wired into the relevant flow, follows the applicable project constraints, handles important error or edge cases, and has appropriate verification or clear evidence.
- **`[/] Partially implemented`**: Some structure or behavior exists, but important branches, integration, persistence, validation, error handling, configuration, tests, or runtime wiring are missing or incomplete.
- **`[ ] Unimplemented`**: The reference item exists in the source material, but no meaningful implementation evidence is found.
- **`[?] Unclear`**: Evidence is insufficient or the reference item is ambiguous. Use this sparingly and explain exactly what must be checked or clarified.
- **`[!] Diverged / docs stale`**: The implementation intentionally or materially differs from the reference source, or the reference source appears outdated. Explain which side likely needs to change.

### 3. Produce The Audit Report

Unless the user asks for a file, report directly in the conversation. If the report is large or meant to be kept, write it to an appropriate project artifact path and mention the file.

The report should include:

#### Audit Meta

- **Reference sources**: Link to the documents, issues, specs, or context used.
- **Implementation inspected**: Link to the relevant files, modules, tests, schemas, configs, or runtime surfaces.
- **Assumptions**: State important scope assumptions and any sources that were unavailable.

#### Progress Comparison Table

| Requirement / Design Point | Reference Source | Implementation Evidence | Status | Gap / Notes |
| :--- | :--- | :--- | :---: | :--- |
| Example behavior | `spec.md` section or line | `service.go`, test, config, API route, or none found | `Implemented` | Aligned with the reference. |
| Example partial item | `planning.md` section or line | `model.go` only | `Partially implemented` | Model exists, but runtime wiring and tests are missing. |
| Example stale doc | `adr.md` section or line | Current code follows a different contract | `Diverged / docs stale` | Decide whether to update docs or change code. |

#### Gap Reflections

- **Implementation gaps**: What behavior or integration is missing, and what risk does it create?
- **Design/document gaps**: Which reference items are ambiguous, outdated, over-specified, or inconsistent with current implementation choices?
- **Architectural or process drift**: Where did the code drift from the intended boundary, contract, or workflow, and why might that have happened?

#### Actionable Next Steps

Provide a prioritized task list. Each task should include:

- The concrete change needed and the likely files/modules/docs involved.
- The acceptance criteria.
- The verification method, such as tests, static checks, build commands, manual checks, screenshots, API probes, or document consistency review.

## Optional Review-Fix-Rereview Integration

When the user explicitly asks to refine the audit through a review-fix-rereview loop:

1. **Review**: Re-check whether each status is supported by evidence, whether links are precise enough, whether unimplemented/partial items are not overstated, and whether next steps directly address the discovered gaps.
2. **Fix**: Correct inaccurate classifications, add missing evidence, clarify assumptions, and tighten the action plan.
3. **Re-review**: Read the revised audit from scratch and repeat until no actionable inaccuracies remain or the agreed iteration budget is reached.

## Reporting Guidance

- Follow the user's language and project instructions for output language and tone.
- Prefer precise file references over broad claims.
- Do not mark something as implemented solely because a type, stub, TODO, or mock exists.
- Distinguish missing implementation from intentionally changed design.
- If code is correct and docs are stale, say so directly and recommend a documentation update.
- If docs are aspirational and code intentionally lags behind them, classify the item as partial or unimplemented rather than treating the code as wrong by default.
- Keep the report proportional: summarize low-risk completed items and spend detail on gaps, drift, and decisions.
