# duplicate_finder

>*WARNING*: This tool is in early development and may have bugs. Use at your own risk.

A fast, safe duplicate-file scanner for macOS/Linux. Uses a three-stage pipeline (size grouping → partial hash → full hash) to avoid unnecessary I/O. Dry-run by default; no files are deleted unless `-delete` is explicitly passed.

## Usage

```
find_duplicates [options] PATH
```

## Safety guarantees

- Dry-run is the default; `-delete` must be explicit.
- At least one copy of every hash group is always kept.
- Files inside `-origin` are **never** deleted regardless of other flags.
- Every deletion decision is traceable to: full-hash equality + path policy + preference rules.

### Options

| Flag | Default | Description |
|---|---|---|
| `-path PATH` | – | Root directory to scan (or positional arg) |
| `-origin PATH` | – | Protected directory — copies here are always kept |
| `-likely_duplicates PATH` | – | Prefer deleting copies found here |
| `-exclude PATTERN` | – | Glob pattern to exclude (matches relative path **and** basename); repeatable |
| `-only EXTS` | – | Only scan these extensions, comma-separated (e.g. `jpg,png,mp4`) |
| `-mode MODE` | `exact` | Scan mode: `exact` (byte-identical) or `near-image` (perceptual hash) |
| `-threshold N` | `10` | Hamming distance threshold for `-mode near-image` (0 = identical, 64 = max) |
| `-hash-algo ALGO` | `blake3` | Hash algorithm: `sha256`, `xxh3`, `blake3` |
| `-mmap` | `false` | Use mmap for full hashing — only enable on fast local SSDs; harmful on NAS/spinning disks |
| `-db PATH` | – | SQLite cache path — avoids re-hashing unchanged files (~200 MB / 1 M files) |
| `-workers N` | NumCPU | Parallel hash workers (reduce to 1–2 for HDD or NAS) |
| `-min-size BYTES` | `1` | Minimum file size to consider (default skips zero-byte files) |
| `-max-size BYTES` | `0` | Maximum file size to consider (`0` = unlimited) |
| `-delete` | false | **Actually delete** files (default is dry-run) |
| `-trash` | false | Move files to `~/.Trash` instead of permanently deleting (macOS) |
| `-delete-empty-dirs` | false | Remove directories emptied by the deletion run |
| `-json` | false | Write `report.json` / `report_near.json` (near-image mode) |
| `-csv` | false | Write `report.csv` / `report_near.csv` (near-image mode) |
| `-scan-id ID` | auto | Name this scan in SQLite for later resume (requires `-db`) |
| `-resume` | false | Reload the file list from a previous `-scan-id` scan (requires `-db`) |
| `-help` / `-h` | – | Show help |

### Examples

```sh
# Dry-run scan — just report what would be deleted
./find_duplicates /Volumes/Photos

# Protect originals; mark likely-duplicate folder for deletion
./find_duplicates \
  -origin /Volumes/Photos/Masters \
  -likely_duplicates /Volumes/Photos/Exports \
  /Volumes/Photos

# Use SQLite cache, limit workers for slow NAS, exclude thumbnails
./find_duplicates \
  -db ~/.cache/dupfinder.db \
  -workers 2 \
  -exclude "*.thumb" \
  -exclude ".DS_Store" \
  /mnt/nas/media

# Scan only photos, move dupes to Trash
./find_duplicates -only jpg,png,heic -trash /Volumes/Photos

# Near-image mode: find visually similar photos (not just byte-identical)
./find_duplicates -mode near-image -threshold 8 /Volumes/Photos

# Near-image + JSON report + move to Trash
./find_duplicates -mode near-image -threshold 10 -json -trash /Volumes/Photos

# Resumable scan using SQLite cache
./find_duplicates -db ~/.cache/scan.db -scan-id photos_2026 /Volumes/Photos
# ... interrupted ... resume:
./find_duplicates -db ~/.cache/scan.db -scan-id photos_2026 -resume /Volumes/Photos

# Delete + remove empty directories + CSV report
./find_duplicates -delete -delete-empty-dirs -csv /Volumes/Backup

# Limit to files under 500 MB, write JSON report
./find_duplicates -max-size 524288000 -json /Volumes/Backup
```

## Performance
Results running with 1-8 workers. 
Not much benefit in my set-up as the DiskIO was limiting after 2 workers.

```
Scanned           : 17262 files in 5m42.377526815s
Scanned           : 17262 files in 2m53.893585607s
Scanned           : 17262 files in 2m25.548567898s
Scanned           : 17262 files in 2m8.680930305s
Scanned           : 17262 files in 2m7.64361788s
Scanned           : 17262 files in 2m6.882060952s
Scanned           : 17262 files in 2m9.088481311s
Scanned           : 17262 files in 2m12.577234642s
```

## Near-image mode

Near-image mode finds **visually similar** images — photos that are not byte-identical but look alike. Use it for:
- The same photo saved at different quality levels or resolutions
- Photos that were cropped, rotated, or slightly recoloured
- WhatsApp/social-media re-encodes of originals

### How it works

1. **Walk** — same as exact mode: size-group all image files (jpg, jpeg, png, gif, tif, tiff).
2. **Perceptual hash** — each image is decoded into a bitmap and a 64-bit **dHash** (difference hash) is computed by scaling the image to 9×8 pixels and comparing adjacent pixels.
3. **Hamming distance grouping** — every pair of images is compared by counting differing bits (`XOR` + `popcount`). Pairs with distance ≤ `-threshold` are grouped together via union-find.
4. **Sort & keep** — within each group the same path-policy and preference rules as exact mode apply (origin, likely_duplicates, resolution, EXIF date).

### Threshold guide

| `-threshold` | Meaning |
|---|---|
| `0` | Pixel-identical after resize — essentially exact duplicates |
| `1–5` | Near-identical; only trivial compression differences |
| `6–10` | *(default)* Slight re-encodes, minor crops, watermarks |
| `11–20` | Filtered/edited versions, colour-graded copies |
| `>20` | Loosely similar scenes — high false-positive rate |

### Expected accuracy

- **Low threshold (0–5):** Very high precision, few false positives. May miss lightly edited copies.
- **Default threshold (10):** Good balance for photo libraries. Expect occasional false positives for photos of very similar scenes (e.g. two consecutive shots of the same subject).
- **High threshold (>15):** Many false positives likely. Review every group manually before deleting.

**Known false-positive sources:**
- The same photo with a colour filter applied (Instagram-style)
- Night/day versions of the same scene
- Two photos of the same landscape, building, or document
- Screenshots of similar UI screens

### Flags that do NOT apply to near-image mode

| Flag | Reason |
|---|---|
| `-hash-algo` | Near-image uses perceptual dHash, not a crypto/checksum hash |
| `-mmap` | Image decoding uses the standard decoder, not mmap |
| `-db` / `-scan-id` / `-resume` | The SQLite hash cache only stores exact-mode hashes; near-image hashes are not cached |

All other flags (`-workers`, `-origin`, `-likely_duplicates`, `-exclude`, `-only`, `-min-size`, `-max-size`, `-delete`, `-trash`, `-delete-empty-dirs`, `-json`, `-csv`) work normally.

### Performance

Near-image mode is **significantly slower** than exact mode:
- Every candidate image is **fully decoded** into a bitmap in memory (~96 MB for a 24 MP photo per worker).
- The grouping step is **O(n²)** — 10 000 images → ~50 M comparisons; 50 000 images → ~1.25 B comparisons.
- Reduce `-workers` on memory-constrained hosts; with the default `NumCPU` workers, peak RAM usage is roughly `workers × avg_decoded_image_size`.

### Sample near-image output

```
Scanned           : 8 400 image files in 14m22s
Similarity groups : 312

--- Group 1  dist:2  2 files ---
  KEEP  /Photos/Masters/IMG_1042.jpg  (4032x3024, 6.1 MiB)
  DEL   /Photos/Exports/IMG_1042_web.jpg  (1920x1440, 1.2 MiB)

--- Group 2  dist:8  3 files ---
  KEEP  /Photos/Masters/DSC_0091.jpg  (4032x3024, 8.4 MiB)
  DEL   /Photos/Filtered/DSC_0091_vivid.jpg  (4032x3024, 5.1 MiB)
  DEL   /Photos/WhatsApp/IMG-20230715-WA0003.jpg  (1600x1200, 0.3 MiB)

Results:
  Similar groups   : 312
  Removable files  : 498
  Potential freed  : 3.2 GiB
```

Groups are written in the same format to `dryrun_near_duplicates.txt` (dry-run) or `near_duplicates.txt` (with `-delete`/`-trash`).

## Performance
Results running with 1-8 workers. 
Not much benefit in my set-up as the DiskIO was limiting after 2 workers.

```
Scanned           : 17262 files in 5m42.377526815s
Scanned           : 17262 files in 2m53.893585607s
Scanned           : 17262 files in 2m25.548567898s
Scanned           : 17262 files in 2m8.680930305s
Scanned           : 17262 files in 2m7.64361788s
Scanned           : 17262 files in 2m6.882060952s
Scanned           : 17262 files in 2m9.088481311s
Scanned           : 17262 files in 2m12.577234642s
```

## Output files

| File | Contents |
|---|---|
| `dryrun_duplicates.txt` | Exact mode dry-run: files that *would* be deleted |
| `duplicates.txt` | Exact mode with `-delete`/`-trash`: files acted on |
| `skipped_duplicates.txt` | Groups overlapping `-origin`; lines prefixed `KEEP:` / `DEL:` |
| `report.json` | Exact mode `-json`: groups, pipeline stats, top dirs |
| `report.csv` | Exact mode `-csv`: group_id, hash, size_bytes, action, path |
| `dryrun_near_duplicates.txt` | Near-image mode dry-run |
| `near_duplicates.txt` | Near-image mode with `-delete`/`-trash` |
| `report_near.json` | Near-image mode `-json`: groups with dHash distance |
| `report_near.csv` | Near-image mode `-csv`: group_id, min_dhash_dist, action, path |

`._*` macOS resource-fork sidecar files are silently skipped (counted in summary, never written to a report).

## Sample output

```
Scanned           : 142857 files in 4.2s
._* skipped       : 312

Pipeline stats:
  Size candidates  : 9840 files (same size as ≥1 other)
  After partial    : 1204 remain, 8636 filtered out
  After full hash  : 388 confirmed duplicates, 816 filtered out

--- Group 1  hash:a1b2c3d4  size:3.2 MiB  2 files ---
  KEEP  /Photos/Masters/IMG_1042.jpg  [origin]
  DEL   /Photos/Exports/IMG_1042.jpg

--- Group 2  hash:e5f6a7b8  size:14.7 MiB  3 files ---
  KEEP  /Photos/Masters/DSC_0091.CR2  [outside likely-duplicates]
  DEL   /Photos/Backup/DSC_0091.CR2
  DEL   /Photos/OldBackup/DSC_0091.CR2

Results:
  Duplicate groups : 134
  Removable files  : 254
  Potential freed  : 12.3 GiB
```

## Deletion policy

1. **No flags** — keep the first file (after preference sort), delete the rest.
2. **`-origin` only** — files inside `-origin` are always kept; outside copies are deleted. If no copy exists in origin, fall back to keeping the first.
3. **`-likely_duplicates` only** — files outside `-likely_duplicates` are kept; copies inside are deleted. If all copies are inside, keep the first.
4. **Both flags** — origin wins; then outside-of-both wins; then likely_duplicates is deleted last.

Within each tier, a preference sort pushes higher-deletion-priority files to the front: `.pending_*` → `._*` → copy-variants (`(1)`, ` Copy`, `_1`) → edited/filtered → WhatsApp → JPEG (when RAW exists).

## Preference heuristics (delete-first order)

1. `.pending_` prefix in path
2. `._` macOS resource fork in path
3. Copy variants: `(N)`, ` Copy`, `_N` suffix
4. Edited / filtered variants
5. WhatsApp-named files
6. `.jpg`/`.jpeg` when a higher-priority copy exists

## Build

**Requires Go 1.23+** (Debian's packaged `golang-1.19` is too old; install from [go.dev/dl](https://go.dev/dl/)).

```sh
make build
# or
go build -o find_duplicates .
```

## Development

```sh
make test    # run all tests
make vet     # static analysis
make fmt     # gofmt
make tidy    # go mod tidy
make all     # tidy + fmt + vet + test + build
```

### Repository layout

```
.
├── find_duplicates.go          # main: CLI, walk, orchestration
├── find_duplicates_nearimage.go # near-image mode: runNearImageMode
├── internal/
│   ├── cache/cache.go          # SQLite inode-cache helpers
│   ├── hashing/hashing.go      # partial + full hash (mmap / streaming)
│   ├── nearimage/nearimage.go  # perceptual hash, EXIF, similarity grouping
│   └── rules/
│       ├── rules.go            # path-policy (SelectKeepDelete), preference sort
│       └── rules_test.go       # unit tests for all 4 policy combinations
├── .github/
│   ├── workflows/
│   │   ├── ci.yml              # test + vet on every push / PR
│   │   └── build-rc.yml        # manual RC: cross-compile + artifact upload + git tag
│   ├── ISSUE_TEMPLATE/
│   ├── pull_request_template.md
│   └── dependabot.yml
├── go.mod
├── Makefile
├── CHANGELOG.md
├── CONTRIBUTING.md
├── SECURITY.md
├── LICENSE
├── .editorconfig
└── README.md
```


