# Web app

`deploy-web.yml` exports `apps/mobile` for web on a push under `apps/mobile/**`, releases it to staging with `deploy/web-release.sh`, smoke-tests it, and after a `production` approval does the same for prod. Caddy serves a file from `$WEB_ROOT/<tier>/current` when the path matches one, and go-api otherwise. `<tier>` is `staging` or `prod`. Caddy mounts `~/altune-web` read-only, the default `WEB_ROOT`.

```bash
ls -t "$WEB_ROOT/<tier>/releases"
bash deploy/web-release.sh <tier> <previous-sha>
bash deploy/web-release.sh <tier> <sha> /path/to/web.tgz
rm "$WEB_ROOT/<tier>/current"
```

In order: list releases newest first, roll back, release a tarball by hand, and turn web off so every path goes to go-api. Five releases are kept; a pruned sha needs its workflow re-run.

A build that fails on an inline script hash names a `sha256`: add it to that tier's `script-src` in `deploy/Caddyfile`.

On a new VM, run `mkdir -p "$WEB_ROOT"` as the deploy user before `docker compose -f deploy/compose.prod.yml up -d caddy`.

To run `npx expo export -p web` locally, copy `apps/mobile/.env.example` to `apps/mobile/.env` and fill it in. `EXPO_PUBLIC_SUPABASE_URL` and `EXPO_PUBLIC_SUPABASE_ANON_KEY` come from the staging Supabase project's dashboard (Project Settings, API): the project URL and the publishable anon key. `EXPO_PUBLIC_API_URL` is `http://localhost:8000` for a local go-api, or the staging host. The export fails at start-up when the Supabase pair is unset.
