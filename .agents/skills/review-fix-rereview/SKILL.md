---
name: review-fix-rereview
description: Manual-only workflow for iterative review and closure of engineering work products, including code, architecture designs, implementation plans, specs, and docs, with explicit first-principles analysis ("从第一性原理分析") before judging fixes. Use only for explicit manual trigger phrases such as $review-fix-rereview, "调用审查技能", "使用审查技能", "从第一性原理分析", naming this skill, or asking to use the "review-fix-rereview" / "审查-修复-再审查" loop. Do not use for ordinary coding, review, design, refactor, or debugging requests unless the user explicitly asks to invoke this skill.
---

# Review Fix Rereview

## Core Rule

1. Run an iterative "review -> fix -> re-review" loop until no actionable issues remain, within a bounded iteration budget.
2. **Manual trigger only**: Do not start this closed loop automatically after ordinary coding, design, refactoring, or documentation work. Use it only when the user explicitly invokes this skill or asks for a review-fix-rereview cycle.
3. **First-principles analysis**: Before deciding whether something is correct or worth fixing, reduce the artifact to its essential goal, invariants, constraints, user value, and failure conditions. Use this "从第一性原理分析" step to avoid inheriting flawed assumptions from the current implementation, plan, or wording.
4. An actionable issue is one that materially affects correctness, requirement fit, safety, architecture boundaries, maintainability, extensibility, feasibility, verification, or the user's stated goal. Style preferences, tiny wording choices, speculative improvements, and low-value polish do not keep the loop alive unless they block understanding, consistency, or acceptance.
5. Do not stop after the first fix or revision when material issues remain. Also do not chase endless perfection: once remaining items are non-actionable polish or accepted residual risk, stop and report them clearly.

## Iteration Budget

- Expect most work to converge in 1-3 review cycles. Use a 4th or 5th cycle only when actionable issues still remain and another focused revision is likely to resolve them.
- Do not exceed 5 cycles without explicit user approval. If actionable issues still remain after the 5th cycle, stop, summarize what remains, explain why it did not converge, and recommend the smallest next step.

## Loop

1. **Establish scope.**
   Identify the artifact under review, the user's intent, relevant context, constraints, existing patterns, project instructions, and the evidence needed to judge completeness.
   - **From first principles**: State the core problem, the minimum necessary outcome, non-negotiable constraints, key invariants, and what would make the artifact fail in practice. Use this baseline to challenge inherited assumptions before applying repository or artifact-specific criteria.
   - **Design-implementation alignment**: If reviewing implementation work, check whether it diverges from the accepted design, implementation plan, requirements, or task constraints.
   - **Single-artifact focus**: Unless the user asks otherwise, stay focused on the artifact type under review. For example, when reviewing a design, do not default to reviewing code.

2. **Choose the review lens.**
   Adapt the criteria to the artifact:
   - **Design & Architecture Review**:
     - **First-principles problem framing**: Is the design solving the real root problem instead of optimizing around incidental current structure, legacy assumptions, or premature implementation choices?
     - **Problem fit & tradeoffs**: Does the design solve the core problem, are assumptions explicit, and are tradeoffs reasonable?
     - **Module boundaries & decoupling**: Are responsibilities clear, and does the design follow the current repository's architecture boundaries, dependency direction, module/product isolation rules, and established abstraction style? Use project-level instructions, design documents, and existing patterns as the source of truth.
     - **Data flow, feasibility & failure modes**: Are control/data flows coherent, and are error handling, concurrency, rollout, migration, and failure scenarios considered where relevant?
   - **Code Review**:
     - **First-principles correctness**: Does the code preserve the essential invariants and contract of the problem, independent of how the previous code happened to be shaped?
     - **Completeness & alignment**: Does the implementation satisfy the accepted requirements, design, or task plan without important omissions?
     - **Architecture & directory structure**: Is code placed according to the current project's package/module conventions, and are dependency boundaries respected?
     - **Maintainability & extensibility**: Are modules cohesive, coupling controlled, abstractions justified, and future changes reasonably easy?
     - **Clarity & idiomatic style**: Are names, control flow, error handling, and APIs clear and consistent with the language and repository conventions?
     - **Performance & safety**: Are hot paths, concurrency, resource lifetime, validation, permissions, and security risks handled proportionally to the artifact's risk?
     - **Project standards**: Does the work follow the repository's language, testing, formatting, persistence, tenancy, observability, and verification rules? Treat project-level instructions and existing code conventions as authoritative.
   - **Plan or task breakdown**: Check completeness, sequencing, dependencies, risks, validation points, rollback or contingency, and whether each step is actionable.
   - **Specs or docs**: Check clarity, consistency, missing cases, contract accuracy, ambiguity, reader workflow, and alignment with implementation intent.

3. **Review for material risk.**
   Prioritize correctness, completeness, elegant design, best practices, clear contracts, maintainable structure, and future extensibility. When uncertain, reason from first principles: what must be true for this artifact to satisfy the user's goal, and what concrete failure would occur if it does not? Classify findings as actionable, optional polish, or residual risk. Only actionable findings drive another fix cycle.

4. **Apply contextual judgment.**
   Add deeper checks only when the artifact calls for them: security for untrusted input or external execution; permissions/auth/tenancy/sandboxing for access-control or tool/file/command paths; observability for long-running, distributed, operational, or hard-to-debug flows; performance for hot or blocking paths; API/data/docs impact when contracts or persistence change.

5. **Revise deliberately.**
   Fix actionable findings with focused changes that address root causes. Avoid unrelated rewrites and avoid expanding scope to minor improvements unless they are necessary for a clean, extensible result.

6. **Verify proportionally.**
   Use the right evidence for the artifact: tests or static checks for code, consistency checks for designs and specs, feasibility and dependency checks for plans, and manual validation when automation is not appropriate. If verification cannot be completed, explain why.

7. **Re-review from scratch.**
   Re-read the revised artifact as if reviewing another engineer's work. If actionable findings remain and the iteration budget allows, repeat the loop. If only optional polish remains, stop and mention it briefly without continuing to edit.

## Completion Criteria

Finish when:

- No known actionable findings remain for the requested artifact, or the 5-cycle limit has been reached and remaining issues are reported.
- Verification is proportional to the artifact type and risk.
- The result is correct, coherent, maintainable, extensible, and aligned with project boundaries and best practices, unless explicitly listed as residual risk.
- Optional polish does not materially improve the user's goal, or is explicitly deferred.
- Any residual risk is explicitly named and justified.
- The final response summarizes how many cycles ran, what changed, how it was verified, and whether risk remains.

## Guidance

Keep the loop strict for material issues, restrained for minor ones, and bounded by the iteration budget. Do not force code-review criteria onto designs or plans, do not force unrelated dimensions onto small isolated changes, and do not optimize beyond the user's scope. Do not invent certainty; investigate, prove not applicable, defer as optional polish, or label residual risk.
