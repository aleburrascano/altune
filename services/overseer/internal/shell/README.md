`shell` decides which dashboard routes the owner may call and what they answer.

- The HTTP routes live in `internal/adapters/httpapi`. `/health` is public and minimal, `/config.json` and the dashboard files are public, and every `/api/` route needs the owner's bearer token.
- The `/api/` routes serve the sorted bucket snapshots, a bucket's series over a range, and the live stream of frames.
- A bucket that panics or whose source is down is still served and the shell stays up.
- The owner health route also reports the last collect cycle and the state of the go-api credential, never its secrets.
