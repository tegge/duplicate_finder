# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `-mode near-image`: perceptual-hash near-duplicate detection for photos (dHash + pHash via `goimagehash`, EXIF metadata, filename-family grouping, union-find similarity clustering)
- `-threshold N`: Hamming distance threshold for near-image mode (default 10, range 0-64)
- `-resume` + `-scan-id ID`: resumable scans stored in SQLite — re-run on an interrupted scan without re-walking the filesystem
- `-delete-empty-dirs`: removes directories emptied by a deletion run (deepest-first)
- `-csv` flag: writes `report.csv` (exact mode) or `report_near.csv` (near-image mode)
- `-json` flag: writes a structured `report.json` alongside the text report
- `-only EXTS` flag: restrict scan to specific file extensions (e.g. `jpg,png,mp4`)
- `-max-size BYTES` flag: skip files above a given size
- `-trash` flag: move deletion candidates to `~/.Trash` instead of permanently deleting
- Directory-level duplicate stats in stdout summary (top-10 directories by removable files)
- Pipeline statistics in stdout summary (size candidates → partial hash → full hash funnel)
- `internal/nearimage` package: perceptual hashing, EXIF extraction, filename-family detection, union-find grouping
- `internal/rules` package: `IsUnder`, `SortByPreference`, `SelectKeepDelete`
- `internal/hashing` package: `Partial`, `Full` (mmap + streaming fallback)
- `internal/cache` package: SQLite inode-cache helpers
- Unit tests for all 4 path-policy combinations and preference sorting
- `.github/workflows/ci.yml`: test + vet on every push / PR
- `.github/workflows/build-rc.yml`: manual RC with cross-compile, artifact upload, git tag
- `.github/dependabot.yml`: automated Go module and Actions version updates
- `Makefile`, `README.md`, `.gitignore`, `.editorconfig`

### Changed
- `._*` macOS sidecar files are silently skipped and counted; no longer written to a report file
- `KEEP:` / `DEL:` prefixes in `skipped_duplicates.txt` for machine-readable output
- Replaced `os.ReadFile` with streaming `io.CopyBuffer` for sha256/blake3 full hashing
- `-exclude` now matches against both relative path and basename

### Fixed
- Path comparison uses `isUnder` (separator-aware) instead of raw `strings.HasPrefix` to avoid prefix collisions (e.g. `/foo/bar` vs `/foo/bar_extra`)
- Hardlink deduplication via `seenInodes` map prevents counting the same inode twice
- Walk errors are counted and reported instead of silently discarded

## [0.1.0] - 2026-03-01

### Added
- Initial implementation: 3-stage pipeline (size grouping → partial hash → full hash)
- Hash algorithms: `sha256`, `xxh3`, `blake3`
- `mmap` support with streaming fallback
- SQLite cache (`-db`) for incremental re-scans
- Priority queue: largest files hashed first
- Parallel hash workers (`-workers`)
- Path policies: `-origin` (protected), `-likely_duplicates` (prefer delete)
- Preference heuristics: `.pending_`, `._*`, copy variants, edited, WhatsApp, JPEG
- Dry-run default; `-delete` required for actual removal
- `-min-size` filter
- Skip dirs: `node_modules`, `site-packages`, `__pycache__`, `.venv`, `.cache`
