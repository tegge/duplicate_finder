# Contributing

## Development setup

```sh
git clone https://github.com/effem/duplicate_finder
cd duplicate_finder
go mod tidy
make build
make test
```

## Workflow

1. Fork the repository and create a branch from `main`.
2. Make your changes, add tests where appropriate.
3. Run `make all` (tidy → fmt → vet → test → build) before pushing.
4. Open a pull request against `main`.

## Code style

- `gofmt` is enforced. Run `make fmt` before committing.
- `go vet` must pass. Run `make vet`.
- Keep the 3-stage pipeline (size → partial hash → full hash) intact.
- New flags must have sensible defaults that preserve existing behaviour.

## Running tests

```sh
make test
# or with race detector
go test -race -count=1 ./...
```

---

# Instructions for AI contributors

## Goal

Maintain and extend a safe, deterministic duplicate-file finder for large file trees.

## Non-negotiable rules

- Never change delete semantics without preserving dry-run safety.
- Never move files from keep to delete implicitly during preference sorting.
- Grouping, selection, and reporting must stay logically separated.
- Any deletion decision must be explainable from:
  1. full-hash equality
  2. path policy (`-origin`, `-likely_duplicates`)
  3. explicit preference rules

## Safety constraints

- Dry-run must remain the default.
- Every delete candidate must belong to a full-hash duplicate group.
- At least one copy of each hash group must remain.
- Protected origin paths must never be deleted unless explicitly re-designed and documented.

## Performance constraints

- Preserve the 3-stage flow: size grouping → partial hash → full hash.
- Avoid full-file reads when cache or mmap already solves the problem.
- Design for millions of files.

## Required outputs

- Summary on stdout (scanned count, pipeline funnel stats, results, top dirs)
- Dry-run / delete file list (`dryrun_duplicates.txt` / `duplicates.txt`)
- Skipped / protected group report (`skipped_duplicates.txt`, `KEEP:` / `DEL:` prefixed)
- Optional JSON report (`report.json` via `-json`)

## Package layout

```
internal/rules/    path-policy logic + preference sort — unit-tested
internal/hashing/  partial + full hash (mmap / streaming)
internal/cache/    SQLite inode-cache helpers
find_duplicates.go main: CLI, walk, orchestration
```

## Preferred next work

- `--resume` / `--scan-id`: resumable scan state in SQLite
- CSV output alongside JSON
- `--delete-empty-dirs`: remove directories emptied by the run
- Perceptual / near-duplicate detection (separate mode, not mixed with exact)
