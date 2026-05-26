# Feature template

When `/feature-spec <name>` creates a new feature folder under `apps/mobile/src/features/<name>/`, it copies the structure of this `_template/`:

```
<name>/
├── CLAUDE.md          # feature-local context (this template ↑)
├── ui/                # screens + feature components
├── hooks/             # feature hooks
├── api/               # client calls to backend (typed via @shared/api-client)
├── types.ts           # types shared *within* this feature
└── __tests__/         # unit/component tests for this feature
```

**Do not edit `_template/` to make changes to a specific feature** — make the changes in the feature's own folder.
