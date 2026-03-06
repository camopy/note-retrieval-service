# Note Retrieval Service

Read-only Golang API for ChatGPT Actions backed by a local Obsidian vault and `notesmd-cli`.

## Environment

Required:

- `NRS_LISTEN_ADDR`
- `NRS_API_KEY`
- `NRS_VAULT_PATH`

Optional:

- `NRS_VERSION` default `dev`
- `NRS_LOG_LEVEL` default `info`
- `NRS_VAULT_NAME` default `main`
- `NRS_NOTESMD_CLI_BIN` default `notesmd-cli`
- `NRS_NOTESMD_CLI_HOME` default `.runtime/notesmd-home`
- `NRS_NOTESMD_CLI_TIMEOUT_MS` default `5000`

## Run

```bash
export NRS_LISTEN_ADDR="127.0.0.1:8787"
export NRS_API_KEY="dev-secret"
export NRS_VAULT_PATH="/absolute/path/to/vault"

go run ./cmd/nrs
```

## Routes

Public:

- `GET /health`
- `GET /openapi.yaml`

Bearer auth required:

- `POST /folders/read`
- `POST /notes/list`
- `POST /notes/read`
- `POST /notes/frontmatter`
- `POST /search`

## Tests

```bash
go test ./...
```
