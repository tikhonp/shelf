# Docs

These docs describe shelf as it runs now.

The [authentication design](plans/simple-auth.md) records the agreed plan and review. [auth.md](auth.md) describes its implementation.

[agents.md](agents.md) describes optional external agents, the review inbox, and shared memory. The [feature draft](plans/library-agent.md) and [implementation contract](plans/library-agent-contracts.md) record the design discussion.

The [sign-in and agent review redesign](plans/signin-and-agent-review-design.md) describes new screens for account sign-in, Account and Agents on Status, and proposal review. It also lists the bugs the screens had before it. The redesign was built on 2026-11-10. [The second pass](plans/signin-and-agent-review-second-pass.md) from the same day is built too and lists the bugs its review found. Both run in production.

[Album History](plans/album-history.md) shows one album's changes on the desktop and the phone. It was designed and built on 2026-11-12 and runs in production. The plan lists the screens, the decisions, the API and the steps.

| Doc | What it holds |
| --- | --- |
| [status.md](status.md) | What runs in production, the remotes, and notes for later work. |
| [vision.md](vision.md) | What Music Shelf is for and what it does. |
| [decisions.md](decisions.md) | The decisions in force and why. |
| [architecture.md](architecture.md) | Containers, modules, source interfaces, downloads, events, the HTTP API, configuration. |
| [agents.md](agents.md) | Optional agent connections, proposals, field review, feedback, and shared memory. |
| [auth.md](auth.md) | Optional accounts, passkeys, sessions, CLI commands, and authors in existing history. |
| [data-model.md](data-model.md) | Tables and their rules. |
| [tags.md](tags.md) | Fields, formats, the rule every tag write follows, history, suggestions, Navidrome's view, the index and the checks. |
| [sources/qobuz.md](sources/qobuz.md) | The Qobuz module and the old bot's import. |
| [sources/apple-music.md](sources/apple-music.md) | The Apple Music module, amdl, and shelf-wrapper with sign-in. |
| [telegram.md](telegram.md) | The bot's flow and texts. |
| [provenance.md](provenance.md) | The `shelf.txt` format. |
| [deployment.md](deployment.md) | Compose services, the gateway, permissions, backups. |
| [manual-checklist.md](manual-checklist.md) | End-to-end checks on blackberry. |
| [ui/design/](ui/design/README.md) | The design: screens, stylesheets, and the rules the app follows. |
