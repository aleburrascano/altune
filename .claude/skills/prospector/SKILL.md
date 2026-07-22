---
name: prospector
description: Surface what to build next in any project — features, improvements, expansions, adjacent bets, and the enabling work to unlock them, each grounded in the actual code. Use when the user asks what to add or build next, how a project could grow or expand, or wants a feature/opportunity review of a codebase.
---

# Prospector

Mine a project for its highest-value next moves. Two phases: **discover broadly, then prospect with grounding.** Never skip discovery — ungrounded ideas are filler, and filler is the whole failure mode here.

## Rules of engagement

- **Answer the question actually asked.** The rules below describe the default shape for an open-ended "what should I build next?" A narrower ask — "am I ready to ship?", "what's riskiest here?", "top 3?" — resets the frame, and these defaults bend to it. The axes, the effort tags, the both-horizons sweep: that's scaffolding for breadth. On a narrow question it reads as padding, and padding is what makes someone stop trusting the answer.
- **Infer the project at runtime.** Assume nothing about architecture, product shape, or maturity. A CLI, a React app, a library, a scraper, and a half-finished experiment each want different moves. Read before you reason.
- **Grounded only.** Every suggestion cites concrete evidence from *this* project — a `path:line`, a stub, a TODO, a gap between what exists and what the code clearly wants to become. No "why here", no entry.
- **Cut the generic.** An idea that would apply to any repo ("add logging", "write docs", "add CI") is filler *unless* this project concretely lacks it and it unblocks something named.
- **No artificial shortlist.** Do not trim to "top 3", "quick wins", or a tidy handful unless the user asks to prioritize. Report the full landscape.
- **Name the wall.** For anything non-trivial, state blockers, dependencies, and prerequisite work. An opportunity behind a wall is only real once you name the wall.
- **Both horizons.** On an open-ended ask: near-term next steps *and* larger expansion / adjacent bets. Don't collapse into one.

## Phase 1 — Discover

Get a grounded map of what the project *is* and where its edges are. Delegate to the **surveyor** subagent (read-only, its own context) so the main thread stays clean for synthesis:

> Agent tool, `subagent_type: "surveyor"`, pointed at the project root or the path the user named. It returns the full recon map.

No surveyor available? Run the sweep inline. Either way the map must answer each of these, with `path:line` evidence:

- **Type & purpose** · **Stack & shape** · **Surface** (touchable API / commands / routes / exports)
- **State of play** — what works vs. what's stubbed or half-built
- **Maturity** — tests, docs, error handling, types, observability
- **Trajectory** — where the code is clearly heading but hasn't arrived

**Done when** every dimension is answered with specifics and the open/unfinished threads are enumerated — not "seems mature" but *which* files, *which* stubs. A vague map yields vague ideas; if the map is thin, dig more before ideating.

## Phase 2 — Prospect

The map gave you leads. Now go get the evidence.

**Read the code before you write the opportunity.** The surveyor compresses — that's what makes it cheap, and it's also what makes it lossy. Summaries smooth over anomalies, and anomalies are where the best moves hide: the comment that contradicts the code beside it, the flag that's read but never written, the README describing a path the code abandoned. A map can tell you what's there; it can't tell you what's *off*. So for each candidate, open the file at the site the map pointed to and read around it. If a `path:line` in your output came from the map rather than your own eyes, you haven't verified it — and an opportunity resting on an unread citation is exactly the ungrounded filler this skill exists to prevent.

Then turn the verified leads into opportunities along the axes below. Include an axis only where this project has real moves on it; add your own axis if the project demands one. Force nothing.

- **Complete the loop** — finish what's started: stubs, half-wired features, missing pieces of an existing flow.
- **Deepen** — make what exists better/faster/more robust: performance, correctness, edge cases, UX polish.
- **Extend** — natural next features that fit the current shape and users.
- **Broaden reach** — new surfaces or audiences: another platform, interface, integration, export/import, API, channel.
- **Adjacent bets** — bigger or lateral opportunities the project is positioned for but hasn't reached. Higher risk, higher ceiling.
- **Foundations** — enabling work that unblocks a *cluster* of the above (a missing abstraction, test harness, data layer, auth, plugin system). Include only tied to what it unlocks.

### Per opportunity — tight and scannable

- **What** — the move in one sharp sentence.
- **Why here** — the specific evidence in *this* project (`path:line`, a stub, a gap). Mandatory.
- **Effort** — rough size (S / M / L) and what it touches.
- **Unblocks / needs** — dependencies, prerequisites, blockers; note when one opportunity enables others.

### Output

Open with a 2–4 line read on what the project is and its current edge. Then the opportunities, grouped by axis, most- to least-grounded within each group. Close with **cross-cutting enabling work** — the foundational pieces that light up multiple ideas at once; these often outweigh any single feature.

**Done when** every axis with real moves is covered, every listed opportunity carries its evidence, and no idea is generic filler. Rank or shortlist only if the user asks — otherwise, the full landscape.
