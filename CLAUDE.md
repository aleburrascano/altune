@README.md

- A task is done when `bash scripts/precheck.sh` prints `precheck: green`. It lints and tests what changed against `gitea/main`. Exit 1 means fix the FAIL lines. Exit 3 means a tool is missing here, and the SKIP lines say which.
- Every Edit or Write is checked by `scripts/check-file.sh` on that file. Fix what it reports before the next edit.
- In a new worktree, run `bash scripts/worktree-deps.sh <project dir>` (for example `client`) before running checks.
