# renpy-tl

Standalone Go tool to fill empty `new ""` entries in Ren'Py translation files.

## Overview

Ren'Py generates translation stubs under `game/tl/<language>/` with the English line commented and an empty `new ""` below. This tool translates those entries through OpenCode Go and processes each file in chunks so model limits are never reached. Only `new ""` is filled, while hashes and `old` remain unchanged.

## Prerequisites

- Go 1.22
- An OpenCode Go API key from https://opencode.ai/auth. You can subscribe to Go and copy the key there.

## Install

```bash
git clone https://github.com/galpt/renpy-tl
cd renpy-tl
go build -o renpy-tl ./cmd/renpy-tl
```

Cross compilation works without cgo.

```bash
GOOS=linux GOARCH=amd64 go build -o renpy-tl ./cmd/renpy-tl
GOOS=windows GOARCH=amd64 go build -o renpy-tl.exe ./cmd/renpy-tl
```

## Configuration

Create `renpy-tl.toml` in the same directory as the binary.

```toml
ai-model = "muse-spark-1.2-contributor"
opencode-api-key = "sk-..."
```

See `renpy-tl.toml.example` for the template. No environment variables are used.

- `ai-model` defaults to `muse-spark-1.2-contributor`. If the model is no longer available, the tool prints a clear error and stops.
- `opencode-api-key` is required for real runs. It is not needed for `--mock` or `--dry-run`.
- The key is never logged.

## Usage

Generate empty translations in Ren'Py first, then run the helper.

Dry preview with mock. No key is needed.

```bash
./renpy-tl --input-folder ./game/tl/german --output-folder ./game/tl/german --translate-to german --dry-run --mock
```

Real run.

```bash
./renpy-tl --input-folder ./input --output-folder ./output --translate-to german
```

If input and output are the same folder, files are updated in place.

Flags.

- `--input-folder` is the path that holds the `.rpy` files.
- `--output-folder` is the path for translated files. It is created if missing.
- `--translate-to` is the target language. It must match `^[a-z0-9_]{2,32}$`. Names like `piglatin` are rejected.
- `--dry-run` does not write files. It only validates.
- `--mock` uses a deterministic `TR: ` prefix and skips the network.

Per file behavior.

- A backup named `name.rpy.bak.<timestamp>` is created before the write.
- A temporary file is created in the same directory, flushed and synced, then atomically renamed. The directory is synced afterwards.
- When rename crosses devices, the tool falls back to copying.
- On Windows the `O_DIRECTORY` case is handled through a fallback open.

Idempotent re-run yields `files_written: 0` when no empty entries remain.

## How it works

- The parser handles BOM, line endings and backslash newline continuation. Size caps are 2 MB per file and 4 KB per string. `HASH_RE` is `_[0-9a-f]{20}_[0-9a-f]{24}(?:_\d+)?` and `TRANSLATE_RE` is `^translate\s+(?P<lang>[a-zA-Z0-9_]+)\s+(?P<ident>.+?)\s*:\s*$`.
- The chunker uses budgets of 500 pairs, 1500 lines and 80000 tokens. Tokens are estimated as `chars/4`. Each file is streamed separately and no block is ever split.
- The validator checks nine invariants and fails closed when any check does not pass. Hash remains unchanged, old remains unchanged, only empty entries are filled, block counts stay steady, escaping is valid through a `quote_unicode` roundtrip, tags form an exact multiset, speaker and suffix are preserved, newline count is preserved, and the file remains parseable after the write.
- The adapter uses `net/http` against `https://opencode.ai/zen/go/v1` and supports both `chat/completions` and `responses` endpoints. It uses strict JSON and a temperature of 0.2.
- The writer uses a two phase batch. It keeps block order and preserves `has_bom`, indent, speaker and suffix.

## Test

```bash
go vet ./...
go build -o renpy-tl ./cmd/renpy-tl
./renpy-tl --input-folder ./testdata/input --output-folder ./testdata/output --translate-to german --dry-run --mock
./renpy-tl --input-folder ./testdata/input --output-folder ./testdata/output --translate-to german --mock
cat testdata/output/sample.rpy
```

Check that only `new ""` became `new "TR: ..."` and that tags like `{b}` and `[player]` remain.

## Troubleshooting

- `error: invalid language: ...` means the language must be 2 to 32 characters from `a-z0-9_` and must not contain path traversal.
- `input folder not found` means you should run from the repo root or pass an absolute path.
- `file too large: ... > 2097152` means the file exceeds 2 MB and should be split.
- `tags mismatch` means skipped entries stay empty for manual review. Check the validator output.
- `opencode-api-key not found` means you should create `renpy-tl.toml` next to the binary.

## License

MIT
