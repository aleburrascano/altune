---
name: mockup
description: Show mockups, diagrams and visual comparisons in a browser tab instead of describing them in text.
disable-model-invocation: true
---

# Mockup

Some questions are answered faster by looking than by reading. This starts a browser companion, writes screens to it, and reads back what the user picked.

It is a tool, not a mode. Starting it does not mean every question goes through the browser.

## What belongs in the browser

The test is whether the content **is** visual, not whether the topic is.

Browser: wireframes, layouts, navigation structures, component designs, architecture diagrams, side-by-side comparisons of two directions, questions about spacing and hierarchy, state machines and flows rendered as diagrams.

Terminal: requirements, scope, conceptual choices between approaches described in words, tradeoff lists, API and data modelling decisions, anything whose answer is words.

"What kind of wizard do you want?" is conceptual — terminal. "Which of these wizard layouts feels right?" is visual — browser.

## Running it

Read [`visual-companion.md`](visual-companion.md) before starting the server. It covers the server lifecycle, the screen directory contract, how selections come back through the event files, and the HTML conventions the frame template expects.

Scripts live in [`scripts/`](scripts/) — `start-server.sh`, `stop-server.sh`, `server.cjs`, `helper.js`, and `frame-template.html`.

Start with `--open` so the user's browser lands on the first screen without being told to.

Done when: the user has seen the screens and their selections have been read back.

## Stop the server

Stop it when the visual questions are finished. A server left running holds a port and a browser tab the user did not ask to keep.
