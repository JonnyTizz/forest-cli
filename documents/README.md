# forest-cli documentation

A tour of the tool, ordered for first-time reading.

| File | What's in it |
|------|--------------|
| [`01-overview.md`](./01-overview.md) | What forest is, the problem it solves, the data model, glossary. |
| [`02-walkthrough.md`](./02-walkthrough.md) | Hands-on, end-to-end run through every command on a fresh playground project. |
| [`03-command-reference.md`](./03-command-reference.md) | Every command and every flag, with a pointer to the implementation file. |
| [`04-code-tour.md`](./04-code-tour.md) | Package-by-package walk through the source code so you can confidently change it. |
| [`05-bubble-tea-tui.md`](./05-bubble-tea-tui.md) | Bubble Tea / Lipgloss / Huh primer, grounded in forest's TUI. |
| [`issues/security.md`](./issues/security.md) | Security review findings. |
| [`issues/correctness.md`](./issues/correctness.md) | Correctness, UX, and code-quality findings (including one real bug). |

Suggested reading paths:

- **"I just want to use it"** → 01 → 02 → 03 (skim).
- **"I want to understand the code I shipped"** → 01 → 02 → 04.
- **"I want to extend the TUI"** → 04 → 05.
- **"What's broken?"** → `issues/security.md` and `issues/correctness.md`.
