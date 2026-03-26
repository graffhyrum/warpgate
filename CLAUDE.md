# Warpgate

Multi-provider infrastructure gateway — unified HTTP API for querying resource state across cloud providers.

## Language & Build

- Go project — use `go build`, `go test`, `make lint`
- Run `make lint` as pre-flight gate before reviews

## Toolkit

| Tool | Purpose                      | Key constraint                                             |
| ---- | ---------------------------- | ---------------------------------------------------------- |
| br   | Issue tracker + triage       | `br doctor -q` at session start; `git push` at session end |
| cm   | Procedural memory (rules)    | `cm context "<task>"` at session start (SessionStart hook) |
| bv   | Graph-aware triage           | Always use `--robot-*` flags (bare `bv` blocks agents)     |
| ms   | Skill discovery              | `ms suggest --machine --cwd .` at session start            |
| cass | Session search (episodic)    | `cass search "<q>" --json --limit 5`                       |
| toon | Token codec (40-60% savings) | Pipe: `--format toon \| toon -d`                           |
| ubs  | Security scanner             | `ubs --diff .` before commits                              |
| dcg  | Destructive command guard    | PreToolUse hook on Bash                                    |

All tools support `--help`.
