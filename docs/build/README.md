# Build contracts

One contract per step of phase 1. A step runs in a fresh context: read its contract and everything under Read first, build it, and finish with green checks.

Each contract has these sections:

- Read first: docs and code to read before starting.
- Done when: the result that ends the step.
- Decisions: topic, decision, why.
- Files: what the step adds or changes.
- Tables, Go signatures and Routes, when the step has them.
- Doc changes: docs that change in the same commits.
- Checklist: each edge case with the name of its test.
- As built: filled in at the end of the step, with what differs from the contract and notes for later steps.

A step is finished when `make check`, `make lint`, `make web-check` and `docker build .` pass, `docs/status.md` is updated, the As built section is filled, and the work is committed on `main` with one-line messages.

| Step | Contract | Depends on |
| --- | --- | --- |
| 1a | [shelf skeleton](1a.md) | |
| 1b | [downloads core](1b.md) | 1a |
| 1c | [qobuz-dl-go](1c.md) | |
| 1d | [Qobuz module](1d.md) | 1b, 1c |
| 1e | [Apple Music module and shelf-wrapper](1e.md) | 1b |
| 1f | [Navidrome and Telegram](1f.md) | 1b |
| 1g | [web UI](1g.md) | 1b to 1f, and the design |
| 1h | [deployment and cutover](1h.md) | all of the above |
