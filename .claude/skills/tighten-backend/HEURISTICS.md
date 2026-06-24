# Heuristics

The vocabulary and probes the fan-out agents apply. Pairs with `/codebase-design` (module, interface, depth, seam, leverage, locality, the deletion test) — this file adds what that skill lacks: the coupling/cohesion taxonomies and the structural tests. Cite the rule or test behind every finding.

## Coupling — name the kind, not just "coupled"

Worst → best [vault: wiki/concepts/Coupling and Cohesion.md]:

1. **Content** — one module reaches into another's internals.
2. **Common** — modules share global / package-level state.
3. **External** — shared externally-imposed format or protocol.
4. **Control** — one passes a flag that steers the other's logic.
5. **Stamp** — passes a fat struct, uses one field.
6. **Data** — communicates through simple params (the goal).

A finding names the kind: _"playback is control-coupled to acquisition via a status flag"_ is actionable; _"they're coupled"_ isn't.

## Cohesion — why a package feels like a junk drawer

Worst → best [vault: wiki/concepts/Coupling and Cohesion.md]:

coincidental → logical → temporal → procedural → communicational → sequential → **functional**.

A `service/` package with 40 flat files is usually **logical** cohesion ("they're all services"), not **functional** ("they serve one task"). Name the level; the fix is sub-grouping toward functional.

## Tests — the probes that surface findings

- **Deletion test** (`/codebase-design`) — delete the unit: complexity _vanishes_ → dead/duplicate/shallow; _ripples_ → coupled. Scales from a function to a whole context.
- **Acyclic-dependency test** — draw the import graph between contexts. A **cycle** means neither end can be extracted — the "can I pull out acquisition?" answer.
- **Shotgun surgery** — one logical change forces edits across many modules → coupling too high / responsibility scattered [vault: wiki/concepts/Open-Closed Principle.md].
- **Divergent change** — one module changes for many unrelated reasons → low cohesion; split it.
- **Feature envy** — a function uses another module's data more than its own → it's in the wrong place.
- **Name-without-"and"** — can't name a unit without "and" → it does more than one thing (SRP = functional cohesion).
- **Test-setup pain** — testing X needs standing up Y and Z → X is coupled to them.

## The brake — every card carries the counter-argument

Relentless is not reckless. Before proposing a sever / merge / delete, state why you might _not_:

- **Wrong abstraction > duplication** — _"duplication is far cheaper than the wrong abstraction"_ [vault: wiki/concepts/DRY Principle.md]. Incidental duplication (looks alike today, changes for different reasons) is left alone.
- **Rule of three** — wait for the third occurrence before extracting [vault: wiki/concepts/DRY Principle.md].
- **One adapter = hypothetical seam, two = real** (`/codebase-design`) — don't introduce a seam for a single implementation.
- **Necessary coupling exists** — the goal is _appropriate, localized_ coupling, not zero. A composition root is _supposed_ to depend on everything; that's its job, not a finding.

> Connascence — coupling graded by strength × locality × degree, with the rule _"stronger connascence must be more local"_ — is the sharpest vocabulary here, but is **not in the project vault**. Use it if it helps; flag it as external when you do.
