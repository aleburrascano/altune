# collabcheck

`collabcheck` is the type-aware check for lexicon 3715 lines 23 and 24. It is a `go/analysis` pass, run as `go run ./cmd/collabcheck ./...` from `services/go-api`, and it is the proof that a directory is at zero in place of the coarse counting script `scripts/ci/count-collaborator-options.sh`. It does not see fields set only by constructors that return an interface, nor a nil check written as a `switch` case or on a copy of the field.

It reports two things, in non-test files only:

- a nil check (`x.f == nil`, `x.f != nil`) on a field that holds an interface or func collaborator and that a constructor sets. A constructor is a package-level function that returns the struct (or a pointer to it) and sets the field in a composite literal or an assignment. Only unexported fields count, so a refusal that reads an exported field of a config struct (`Collaborators.Artwork == nil`) is not reported, and neither is a pointer-typed field.
- a `With*` option (a `With` function or method that returns a func type, or returns its own receiver) with a parameter that is an interface or func collaborator.

A collaborator type is a func type or an interface with at least one method, other than `error`. `any` and empty interfaces are values, not collaborators. A `With*` function that returns a result and not an option (`providerrequests.WithAuthRetry`) is not an option.

The fix for a hit is a required constructor parameter that the constructor refuses nil for, with the explicit absent adapter chosen in the wiring. The check has no ignore list. Run on the whole module, the hits left are the other owners' areas.
