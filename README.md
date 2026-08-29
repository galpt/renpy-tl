# renpy-tl

Standalone Go tool to fill empty `new ""` entries in Ren'Py translation files.

## Overview

Ren'Py generates translation stubs under `game/tl/<language>/` with the English line commented and an empty `new ""` below. This tool translates those entries via OpenCode Go, chunked per file so model limits are never hit. Only `new ""` is filled, hashes and `old` stay unchanged.

## Prerequisites

- Go 1.22
- OpenCode Go API key from https://opencode.ai/auth (subscribe to Go, copy key)

## Install

```bash
git clone https://github.com/galpt/renpy-tl
cd renpy-tl
go build -o renpy-tl ./cmd/renpy-tl
```

Cross compile without cgo:

```bash
GOOS=linux GOARCH=amd64 go build -o renpy-tl ./cmd/renpy-tl
GOOS=windows GOARCH=amd64 go build -o renpy-tl.exe ./cmd/renpy-tl
```

## Configuration

Create `renpy-tl.toml` in the same directory as the binary:

```toml
ai-model = "muse-spark-1.2-contributor"
opencode-api-key = "sk-..."
```

See `renpy-tl.toml.example` for the template. No environment variables are used.

- `ai-model` defaults to `muse-spark-1.2-contributor` and falls back to the same model if unavailable.
- `opencode-api-key` is required for real runs, not for `--mock` or `--dry-run`.
- The key is never logged.

## Usage

Generate empty translations in Ren'Py first, then run:

Dry preview with mock (no key needed):

```bash
./renpy-tl --input-folder ./game/tl/german --output-folder ./game/tl/german --translate-to german --dry-run --mock
```

Real run:

```bash
./renpy-tl --input-folder ./input --output-folder ./output --translate-to german
```

If input and output are the same folder, files are updated in place.

Flags:

- `--input-folder` path with `.rpy` files
- `--output-folder` path for translated files, created if missing
- `--translate-to` target language, must match `^[a-z0-9_]{2,32}$`, `piglatin` and similar are rejected
- `--dry-run` do not write, validate only
- `--mock` use deterministic `TR: ` prefix, skips network

Per file behavior:

- backup `name.rpy.bak.<timestamp>` before write
- temp file in same directory, flushed and synced, atomic rename, directory fsync
- EXDEV fallback copies when rename crosses devices
- on Windows `O_DIRECTORY` is handled via fallback open

Idempotent re-run yields `files_written: 0` when no empty entries remain.

## How it works

- Parser handles BOM, `\r\n` and `\n`, and backslash newline continuation. Size caps are 2 MB per file and 4 KB per string. `HASH_RE` is `_[0-9a-f]{20}_[0-9a-f]{24}(?:_\d+)?` and `TRANSLATE_RE` is `^translate\s+(?P<lang>[a-zA-Z0-9_]+)\s+(?P<ident>.+?)\s*:\s*$`.
- Chunker budgets are 500 pairs, 1500 lines, 80000 tokens estimated as `chars/4`, per file streaming, never split a block.
- Validator checks nine invariants fail closed: hash unchanged, old unchanged, only empty filled, block counts steady, escaping valid via `quote_unicode` roundtrip, tags exact multiset `{b}` `[var]` `%%`, speaker and suffix preserved, newline count, and parseability after write.
- Adapter uses `net/http` against `https://opencode.ai/zen/go/v1`, supports both `chat/completions` and `responses` endpoints, strict JSON, `temperature 0.2`.
- Writer uses two phase batch, keeps block order, preserves `has_bom`, indent, speaker, and suffix.

## Test

```bash
go vet ./...
go build -o renpy-tl ./cmd/renpy-tl
./renpy-tl --input-folder ./testdata/input --output-folder ./testdata/output --translate-to german --dry-run --mock
./renpy-tl --input-folder ./testdata/input --output-folder ./testdata/output --translate-to german --mock
cat testdata/output/sample.rpy
```

Check that only `new ""` became `new "TR: ..."` and tags like `{b}` `[player]` remain.

## Troubleshooting

- `error: invalid language: ...` language must be 2 to 32 chars `a-z0-9_`, no path traversal
- `input folder not found` run from repo root or pass absolute path
- `file too large: ... > 2097152` file exceeds 2 MB, split it
- `tags mismatch` skipped entries stay empty for manual review, check validator output
- `opencode-api-key not found` create `renpy-tl.toml` next to binary

## License

MIT
