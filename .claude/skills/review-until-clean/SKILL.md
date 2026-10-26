---
description: "1-shot to merge-ready (local-only) — Phase A (5-agent local review iteration) → Phase B (push + self-driven Copilot re-review iteration) → Phase C (reply+resolve all threads). One invocation drives a PR from local agent review through Copilot review convergence to merge-ready state. Runs from your own machine; there is no CI auto-fix."
---

Follow the instructions in `.github/prompts/review-until-clean.prompt.md` to perform Phase A+B+C iterative review until merge-ready.

**Model policy**: Phase A's Agent fan-out defaults to `model="sonnet"` (Sonnet 5) for every spawned agent — running the whole skill under Fable has exhausted the token budget before. See the prompt file's "Model policy" section for the Fable-combination exception and the Opus fallback when Fable is unavailable.
