---
description: "Session-end quality checklist — replaces former Stop hooks with passive reminders"
---

# Before ending a session

When the user says "done", "ship it", "that's it", or the task feels complete:

1. **Verify** — Run `/verify-end-to-end` if production code changed and it hasn't been run yet this session.
2. **Commit** — Use `/git-commit` for proper Conventional Commits formatting. The skill's pre-commit checks handle terminology drift and CLAUDE.md hygiene automatically.
3. **Compound learning** — If something surprised you (a non-obvious pattern, not a one-off bug), consider `/compound-learning`.

These are reminders, not gates. Use judgment — routine sessions don't need all three.
