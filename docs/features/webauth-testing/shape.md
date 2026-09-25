# Web/headless test auth (Front-end health prerequisite)

Idea brief. NOT a bucket — the **prerequisite** that unblocks the Front-end health bucket, which stays deferred until this lands.

## Vision
Make the mobile app **drivable by an automated tester**, especially headless/on web, by solving the auth path that currently breaks. Today Supabase auth breaks on Expo web and RN-web ≠ native, so an automated driver can't reach any authenticated screen — which is exactly what a Front-end health prober needs.

## The idea
Provide a **test-only authentication path** that yields a valid session for a **test user** without the broken web OAuth flow, and wire it so an automated driver can use it to render authenticated screens.

**Rejected:** fixing full Supabase web OAuth (heavy, possibly upstream); mocking the whole backend (then you're not testing the real app).

## Assumptions
- **[load-bearing]** A valid session token can be minted for a test user via a **non-production** path and injected into the app so authed screens render.

## Scope / non-goals
**In:** a documented, **non-prod** test-auth path a headless/web driver uses to reach authenticated screens; a working demo of at least one authed screen driven end to end.
**Out:** the Front-end health prober itself (separate, later); any change to production auth; real user credentials.

## Priority
**Must:** a working test-auth path that drives at least one authenticated screen headlessly.

## Invariants
- **Non-production only:** the test-auth path is never usable against prod or real users (a build/flag/host guard makes it impossible in prod).
- Scoped to a dedicated test user.

## Open questions (need a decision)
- **Driver:** `agent-browser` on Expo web, or a native driver (Detox / Maestro)? (Web is what's broken; native sidesteps it but is heavier to stand up.)
- **Mechanism:** a **backend test-login endpoint** (non-prod) that issues a session, or **client-side token injection**? (My lean: a non-prod test-login endpoint + agent-browser on web — reuses the existing UI-testing tooling.)
