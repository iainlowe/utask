# Agents and CLI Integration

This repository provides a Go module and CLI that can be used by both humans and agents, with an optional Model Context Protocol (MCP) server over stdio.

## Module and Binary

- Module: `github.com/iainlowe/utask`
- Binary: `cmd/ut`
  - Build: `go build ./cmd/ut`

## Dependencies

- CLI: `github.com/urfave/cli/v2`
- OpenAI: official Go SDK `github.com/openai/openai-go`
- NATS (KV/JetStream): `github.com/nats-io/nats.go`

## Configuration

- Precedence: config file < environment variables < CLI flags (flags win)
- Default config file: `~/.utask/config.yaml`
- Override via env: set `UTASK_CONFIG` to an alternate path.

Example `~/.utask/config.yaml`:

```yaml
nats:
  url: "neo:4222"
openai:
  api_key: "${OPENAI_API_KEY}"
  model: "gpt-4.1-mini"
ui:
  profile: default
```

### Environment variables

- `UTASK_CONFIG`: path to config file (default `~/.utask/config.yaml`)
- `UTASK_NATS_URL`: overrides NATS URL
- `OPENAI_API_KEY`: OpenAI API key
- `UTASK_OPENAI_MODEL`: overrides model name
- `UTASK_PROFILE`: named profile/namespace (optional)

### Global flags (examples)

- `--config, -c string`: path to config file
- `--nats-url string`: NATS server URL (e.g. `neo:4222`)
- `--openai-api-key string`: OpenAI API key
- `--openai-model string`: OpenAI model
- `--profile string`: profile/namespace for data isolation
- `--verbose, -v`: increase verbosity

## CLI Commands (planned)

- `ut create --title <t> [--tag t ...] [--priority N] [--notes s] [--estimate-min E]` — create task (idempotent via normalized payload)
- `ut list [--tag t] [--status open|closed]` — list tasks
- `ut close <id>` — close task
- `ut reopen <id>` — reopen task
- `ut get <id>` — show task JSON
- `ut events [--since duration]` — stream events (`utask.event.*`)
- `ut tags` — list tags and counts
- `ut mcp --stdio` — run MCP server over stdio

See `utask.md` for schema, normalization, buckets, and events.

## MCP (stdio) Mode

When invoked as `ut mcp --stdio`, the binary runs an MCP server speaking stdio. Intended capabilities:

- Tools: create/list/close/reopen/get tasks, query by tag
- Model provider: uses OpenAI (config/env/flags) for LLM-backed operations if needed
- Config: uses the same precedence rules as the CLI

Notes:
- The process should read/write on stdin/stdout only; no prompts on stderr except logs.
- Graceful shutdown on EOF or signal.

## NATS Defaults

- Default NATS URL: `neo:4222` (configurable in `~/.utask/config.yaml`)
- Buckets/subjects are defined in `utask.md`.

## Return Codes

- `0`: success
- `1`: generic error (I/O, NATS, config)
- `2`: invalid usage/flags

## Implementation Notes

- Use KV compare-and-set for task state transitions; emit events only on successful writes.
- Idempotent task creation using sha512 of normalized payload (see `utask.md`).
- Keep CLI output terse by default; `--verbose` for details, `--json` flags can be added later if needed.
