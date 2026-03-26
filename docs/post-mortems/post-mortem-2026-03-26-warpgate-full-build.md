# Post-Mortem: Warpgate Full Build Session

**Date:** 2026-03-26
**Duration:** ~2 hours (single session)
**Scope:** Full project lifecycle — session recovery, plan adoption, implementation, council review, interviewer evaluation, remediation

## Context

Built a Go portfolio project (warpgate) from a recovered plan through to a pushed, lint-clean, CI-enabled GitHub repo with 12 conventional commits. The project targets a Blizzard Cloud Software Engineer role emphasizing Go, cloud provider aggregation, and distributed systems patterns.

## What Went Well

### Session recovery from directory rename
The previous session was lost when `~/repos/go-demo` was renamed to `~/repos/warpgate`. A background subagent migrated the session history to the new path while implementation began in parallel. No work was lost.

### Plan-first, code-second
The plan (`lazy-herding-thacker.md`) from the prior session had already been stress-tested through 2 iterations. Adopting it directly and executing phase-by-phase produced clean, incremental code with minimal backtracking.

### Council review caught real issues
The 5-expert council (Robert Martin, Inquisitor, Go Idioms, DDD, Security) across 2 rounds surfaced 18 findings — 11 red, 6 yellow, 1 green. The most valuable were:
- **RLock held across provider I/O** — snapshot-then-release pattern was the correct fix
- **GetResource error collapse** — all errors became ErrNotFound; now distinguishes real errors
- **Missing HTTP server timeouts** — slowloris vulnerability
- **Dead `health.Checker` package** — never wired, duplicated logic in handler

### Interviewer evaluation caught what the council missed
The "hiring manager" subagent found the lint failures, unused struct, `containsStr` reimplementation, and `os.Exit` after defer. These were all real issues that would erode interviewer confidence. The lint-clean state is now verified.

### Commit history tells a story
12 conventional commits progress from bootstrap through core/server/CI/docs/TUI/fixes. Each commit is reviewable in isolation.

## What Could Improve

### Lint should have been run before the council review
The council found design issues. The interviewer found lint failures. If `make lint` had been run as a pre-flight check before council review, the lint issues would have been caught 30 minutes earlier and wouldn't have required a separate fix commit.

**Rule candidate:** Run `make lint` (or equivalent) as a pre-flight gate before any council review or expert review.

### The `health` package was dead from creation
`health.Checker` was written, tested, and committed — then never wired into any handler. `handleReady` reimplemented the same logic inline. This was a plan-execution error: the plan listed both `health/checker.go` and the handler, but didn't specify which owns the aggregation. The fix was deletion, but the wasted work could have been avoided by checking wiring during implementation.

**Rule candidate:** After writing a new package, verify it has at least one consumer in `cmd/` or another `internal/` package before committing.

### golangci-lint config had default-enabled checks listed
The initial `.golangci.yml` listed gocritic checks that are already enabled by default, producing 8 warning lines. This is harmless but looks sloppy. The linter's own output flagged it.

### Mock providers didn't check context cancellation
The council DDD expert and the interviewer both flagged that mock providers accepted `context.Context` but never checked `ctx.Err()`. This was a 2-line fix per method but signals that the mocks were treated as "just test data" rather than proper interface implementations.

### README claimed "errgroup fan-out with error propagation" but errgroup's error channel was unused
The `ListAll` and `CheckHealth` closures always return nil. Errors are collected via mutex-guarded slice. Only `GetResource` uses errgroup's actual error-aware semantics. The README phrasing was corrected, but the imprecision could have been caught by re-reading the README against the code before pushing.

## Decisions

| Decision | Rationale |
|----------|-----------|
| Delete `health` package vs. wire it | Dead code is worse than missing code. The handler's inline logic is 4 lines — not worth a separate package. |
| `server.Registry` interface vs. concrete `*provider.Registry` | DIP compliance. Server and health consumers should depend on behavior, not implementation. |
| `run()` returning `int` vs. `os.Exit` in main | Avoids gocritic `exitAfterDefer`. Defers run normally. Exit code propagated cleanly. |
| Exclude `(io.Closer).Close` from errcheck globally | `defer resp.Body.Close()` is ubiquitous in Go. Per-line suppression adds noise. Global exclusion is the community norm. |
| Pin golangci-lint to v2.1.0 | `version: latest` is non-reproducible in CI. Pinned version ensures consistent lint results. |
| Bubbletea TUI over HTMX web dashboard | Go infrastructure role. Terminal tooling aligns better with the target audience than browser dashboards. |

## Metrics

- **Files created:** 19 source files + 6 config/doc files
- **Lines of Go:** ~1,600 (production + test)
- **Test coverage:** config 91%, mock 91%, provider 89%, server 77%, TUI 79%
- **Council findings:** 18 (11 red, 6 yellow, 1 green) — all resolved
- **Interviewer findings:** 6 — all resolved
- **Commits:** 12 conventional commits
- **Dependencies:** 4 direct (errgroup, uuid, bubbletea, lipgloss)

## Follow-up Items

- CI workflow has never actually run green — verify after push (GitHub Actions may need Go 1.26 setup-go support)
- The interviewer rated the project "LEAN HIRE" as a standalone artifact. Resume context and interview performance are the remaining variables.
- No benchmarks or fuzz tests. Low priority for a portfolio demo but would strengthen the "scalable distributed systems" claim.
