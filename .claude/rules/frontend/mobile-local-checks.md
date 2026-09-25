---
paths:
  - "apps/mobile/**"
---

# Mobile — local verification, worktree-specific

- A fresh worktree has no `apps/mobile/node_modules`. Either symlink the main checkout's
  (`ln -s /home/ubuntu/projects/altune/apps/mobile/node_modules apps/mobile/node_modules`,
  untracked, remove after) or install locally: `cd apps/mobile && npm ci --ignore-scripts`
  (a root `npm ci` fails on a native `gyp` build). `npx jest` fails with "Cannot find module
  jest-expo/jest-preset" without one of these.
- `yarn` is not installed. Use `npx` for `jest`, `tsc`, and `eslint`.
- New-code style gate: `cd apps/mobile && node scripts/lint-changed-lines.mjs origin/main`. It
  flags, on any touched line region, functions over 10 lines (including moved code — plain eslint
  misses this), any added/moved/reworded comment (zero-comments rule), and `id-denylist` names
  (e.g. `data`). Run it before pushing; CI's `mobile / lint` enforces the same.
- Dead-code boundary gate is `npx fallow dead-code --fail-on-issues`, not `npm run fallow` (that
  target also reports pre-existing duplication on `main` unrelated to any feature diff).
- CI `cycles` fails if two modules import each other — run `npm run cycles` after moving imports.
