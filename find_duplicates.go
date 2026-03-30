// find_duplicates.go
package main

import (
	"container/heap"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/effem/duplicate_finder/internal/cache"
	"github.com/effem/duplicate_finder/internal/hashing"
	"github.com/effem/duplicate_finder/internal/rules"
	_ "modernc.org/sqlite"
)

type FileJob struct {
	path string
	size int64
}

type result struct {
	path string
	hash string
	size int64
	err  error
}

type PriorityQueue []*FileJob

func (pq PriorityQueue) Len() int            { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool  { return pq[i].size > pq[j].size }
func (pq PriorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *PriorityQueue) Push(x interface{}) { *pq = append(*pq, x.(*FileJob)) }
func (pq *PriorityQueue) Pop() interface{} {
	n := len(*pq)
	item := (*pq)[n-1]
	*pq = (*pq)[:n-1]
	return item
}

type dupGroupJSON struct {
	Hash      string   `json:"hash"`
	SizeBytes int64    `json:"size_bytes"`
	Keep      []string `json:"keep"`
	Delete    []string `json:"delete"`
}

type dirStatJSON struct {
	Dir            string `json:"dir"`
	RemovableFiles int    `json:"removable_files"`
}

type pipelineJSON struct {
	SizeCandidates int64 `json:"size_candidates"`
	AfterPartial   int64 `json:"after_partial"`
	AfterFullHash  int64 `json:"after_full_hash"`
}

type reportJSON struct {
	Scanned              int64          `json:"scanned"`
	ElapsedSeconds       float64        `json:"elapsed_s"`
	WalkErrors           int64          `json:"walk_errors,omitempty"`
	DotUnderscoreSkipped int64          `json:"dot_underscore_skipped,omitempty"`
	Pipeline             pipelineJSON   `json:"pipeline"`
	DuplicateGroups      int64          `json:"duplicate_groups"`
	RemovableFiles       int64          `json:"removable_files"`
	RemovableBytes       int64          `json:"removable_bytes"`
	TopDirs              []dirStatJSON  `json:"top_dirs,omitempty"`
	Groups               []dupGroupJSON `json:"groups"`
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func Usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [options] PATH\n", os.Args[0])
	fmt.Fprintln(os.Stderr, "Options:")
	fmt.Fprintln(os.Stderr, "  -path PATH               Root to scan (or positional)")
	fmt.Fprintln(os.Stderr, "  -origin PATH             Protected directory (keep copies there)")
	fmt.Fprintln(os.Stderr, "  -likely_duplicates PATH  Prefer deleting copies there")
	fmt.Fprintln(os.Stderr, "  -exclude PATTERN         Exclude glob pattern (matches rel path and basename), repeatable")
	fmt.Fprintln(os.Stderr, "  -only EXTS               Only scan these extensions, comma-separated (e.g. jpg,png,mp4)")
	fmt.Fprintln(os.Stderr, "  -mode MODE               Scan mode: exact|near-image (default: exact)")
	fmt.Fprintln(os.Stderr, "  -threshold N             Hamming distance threshold for near-image mode (default: 10, range 0-64)")
	fmt.Fprintln(os.Stderr, "  -hash-algo ALGO          sha256|xxh3|blake3 (default: blake3)")
	fmt.Fprintln(os.Stderr, "  -mmap                    Use mmap for full hashing (default: true)")
	fmt.Fprintln(os.Stderr, "  -db PATH                 SQLite cache path (optional, ~200 MB / 1M files)")
	fmt.Fprintln(os.Stderr, "  -workers N               Parallel hash workers (default: NumCPU; reduce for HDD/NAS)")
	fmt.Fprintln(os.Stderr, "  -min-size BYTES          Minimum file size to consider (default: 1, skips empty files)")
	fmt.Fprintln(os.Stderr, "  -max-size BYTES          Maximum file size to consider (default: 0 = unlimited)")
	fmt.Fprintln(os.Stderr, "  -delete                  Actually delete files (default: dry-run)")
	fmt.Fprintln(os.Stderr, "  -trash                   Move files to ~/.Trash instead of deleting (macOS)")
	fmt.Fprintln(os.Stderr, "  -delete-empty-dirs       Remove empty directories after deletion")
	fmt.Fprintln(os.Stderr, "  -json                    Write JSON report to report.json")
	fmt.Fprintln(os.Stderr, "  -csv                     Write CSV report to report.csv")
	fmt.Fprintln(os.Stderr, "  -scan-id ID              Scan identifier for resume (auto-generated if empty; requires -db)")
	fmt.Fprintln(os.Stderr, "  -resume                  Resume a previous scan by -scan-id (requires -db)")
	fmt.Fprintln(os.Stderr, "  -help, -h                Show help and exit")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case "node_modules", "site-packages", "__pycache__", ".venv", ".cache":
		return true
	default:
		return false
	}
}

func trashFile(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(home, ".Trash", filepath.Base(path))
	if _, err := os.Stat(dest); err == nil {
		ext := filepath.Ext(dest)
		base := strings.TrimSuffix(filepath.Base(path), ext)
		dest = filepath.Join(home, ".Trash", fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
	}
	return os.Rename(path, dest)
}

func main() {
	started := time.Now()

	pathFlag := flag.String("path", "", "Root to scan (or positional)")
	originFlag := flag.String("origin", "", "Protected directory")
	likelyFlag := flag.String("likely_duplicates", "", "Likely duplicates directory")
	var excludes multiFlag
	flag.Var(&excludes, "exclude", "Exclude pattern, repeatable")
	hashAlgo := flag.String("hash-algo", "blake3", "sha256|xxh3|blake3")
	mmapFlag := flag.Bool("mmap", true, "Use mmap for full hashing")
	dbPath := flag.String("db", "", "SQLite cache DB path")
	workers := flag.Int("workers", runtime.NumCPU(), "Number of parallel hash workers (reduce for HDD/NAS)")
	minSize := flag.Int64("min-size", 1, "Minimum file size in bytes to consider (default: 1, skips empty files)")
	maxSize := flag.Int64("max-size", 0, "Maximum file size in bytes (0 = unlimited)")
	deleteMode := flag.Bool("delete", false, "Actually delete files")
	trashMode := flag.Bool("trash", false, "Move files to ~/.Trash instead of deleting (macOS)")
	deleteEmptyDirs := flag.Bool("delete-empty-dirs", false, "Remove empty directories after deletion")
	jsonMode := flag.Bool("json", false, "Write JSON report to report.json")
	csvMode := flag.Bool("csv", false, "Write CSV report to report.csv")
	onlyFlag := flag.String("only", "", "Comma-separated extensions to scan (e.g. jpg,png,mp4)")
	modeFlag := flag.String("mode", "exact", "Scan mode: exact|near-image")
	threshold := flag.Int("threshold", 10, "Hamming distance threshold for near-image mode (0-64)")
	scanIDFlag := flag.String("scan-id", "", "Scan identifier for resume (requires -db)")
	resumeFlag := flag.Bool("resume", false, "Resume a previous scan by -scan-id (requires -db)")
	help := flag.Bool("help", false, "Show help")
	flag.BoolVar(help, "h", false, "Show help")
	flag.Usage = Usage
	flag.Parse()

	if *help {
		Usage()
		return
	}

	args := flag.Args()
	var root string
	if *pathFlag != "" {
		root = *pathFlag
	} else if len(args) > 0 {
		root = args[0]
	} else {
		fmt.Fprintln(os.Stderr, "Error: PATH is required")
		Usage()
		os.Exit(1)
	}

	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		log.Fatalf("failed to normalize root: %v", err)
	}
	root = filepath.Clean(root)
	if !fileExists(root) {
		log.Fatalf("root not found: %s", root)
	}

	origin := ""
	if *originFlag != "" {
		origin, err = filepath.Abs(*originFlag)
		if err != nil {
			log.Fatalf("failed to normalize origin: %v", err)
		}
		origin = filepath.Clean(origin)
		if origin == root {
			log.Fatalf("origin must not equal root")
		}
		if !rules.IsUnder(origin, root) {
			log.Fatalf("origin path %s not under root %s", origin, root)
		}
	}

	likely := ""
	if *likelyFlag != "" {
		likely, err = filepath.Abs(*likelyFlag)
		if err != nil {
			log.Fatalf("failed to normalize likely_duplicates: %v", err)
		}
		likely = filepath.Clean(likely)
		if likely == root {
			log.Fatalf("likely_duplicates must not equal root")
		}
		if !rules.IsUnder(likely, root) {
			log.Fatalf("likely_duplicates path %s not under root %s", likely, root)
		}
	}

	if origin != "" && likely != "" && origin == likely {
		log.Fatalf("origin and likely_duplicates must differ")
	}

	allowedExts := map[string]struct{}{}
	if *onlyFlag != "" {
		for _, e := range strings.Split(*onlyFlag, ",") {
			e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
			if e != "" {
				allowedExts[e] = struct{}{}
			}
		}
	}

	dbEnabled := *dbPath != ""
	var db *sql.DB
	if dbEnabled {
		if fi, statErr := os.Stat(*dbPath); statErr == nil && fi.IsDir() {
			log.Fatalf("-db %q is a directory; provide a file path instead, e.g. -db %s/dupfinder.db",
				*dbPath, strings.TrimRight(*dbPath, "/"))
		}
		db, err = sql.Open("sqlite", *dbPath)
		if err != nil {
			log.Fatalf("failed to open db: %v", err)
		}
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY, inode INTEGER, mtime INTEGER, size INTEGER, hash TEXT)`)
		if err != nil {
			log.Fatalf("failed to init db: %v", err)
		}
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS scans(id TEXT PRIMARY KEY, root TEXT NOT NULL, started_at INTEGER NOT NULL, completed_at INTEGER, status TEXT NOT NULL)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS scan_files(scan_id TEXT NOT NULL, path TEXT NOT NULL, size INTEGER NOT NULL, PRIMARY KEY(scan_id, path))`)
		defer db.Close()
	}

	activeScanID := *scanIDFlag
	if dbEnabled {
		if activeScanID == "" {
			activeScanID = fmt.Sprintf("%s_%d", filepath.Base(root), started.UnixNano())
		}
		if *resumeFlag {
			var status string
			if err := db.QueryRow("SELECT status FROM scans WHERE id = ?", activeScanID).Scan(&status); err != nil {
				log.Fatalf("resume: scan-id %q not found in DB: %v", activeScanID, err)
			}
			fmt.Printf("Resuming scan %s (status: %s)\n", activeScanID, status)
			_, _ = db.Exec("UPDATE scans SET status='in_progress', completed_at=NULL WHERE id=?", activeScanID)
		} else {
			_, _ = db.Exec("INSERT OR REPLACE INTO scans(id,root,started_at,status) VALUES(?,?,?,'in_progress')",
				activeScanID, root, started.UnixNano())
		}
	}

	sizeMap := make(map[int64][]string)
	seenInodes := make(map[uint64]struct{})
	seenDirs := make(map[string]struct{})
	var scannedFiles int64
	var walkErrors int64
	var dotUnderscoreSkipped int64

	walkFn := func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			walkErrors++
			return nil
		}

		if info.IsDir() {
			if shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		base := filepath.Base(path)
		if strings.HasPrefix(base, "._") {
			dotUnderscoreSkipped++
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		for _, pat := range excludes {
			if matched, _ := filepath.Match(pat, rel); matched {
				return nil
			}
			if matched, _ := filepath.Match(pat, base); matched {
				return nil
			}
		}

		if len(allowedExts) > 0 {
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(base), "."))
			if _, ok := allowedExts[ext]; !ok {
				return nil
			}
		}

		if info.Size() < *minSize {
			return nil
		}

		if *maxSize > 0 && info.Size() > *maxSize {
			return nil
		}

		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			if _, seen := seenInodes[st.Ino]; seen {
				return nil
			}
			seenInodes[st.Ino] = struct{}{}
		}

		scannedFiles++
		seenDirs[filepath.Dir(path)] = struct{}{}
		sizeMap[info.Size()] = append(sizeMap[info.Size()], path)
		return nil
	}

	if *resumeFlag && dbEnabled {
		rows, qErr := db.Query("SELECT path, size FROM scan_files WHERE scan_id=?", activeScanID)
		if qErr != nil {
			log.Fatalf("resume: failed to load file list: %v", qErr)
		}
		for rows.Next() {
			var p string
			var sz int64
			if rows.Scan(&p, &sz) == nil {
				scannedFiles++
				seenDirs[filepath.Dir(p)] = struct{}{}
				sizeMap[sz] = append(sizeMap[sz], p)
			}
		}
		_ = rows.Close()
	} else {
		if err = filepath.Walk(root, walkFn); err != nil {
			log.Fatalf("walk failed: %v", err)
		}
		if dbEnabled {
			if tx, txErr := db.Begin(); txErr == nil {
				if stmt, stErr := tx.Prepare("INSERT OR IGNORE INTO scan_files(scan_id,path,size) VALUES(?,?,?)"); stErr == nil {
					for sz, paths := range sizeMap {
						for _, p := range paths {
							_, _ = stmt.Exec(activeScanID, p, sz)
						}
					}
					_ = stmt.Close()
				}
				_ = tx.Commit()
			}
		}
	}

	if *modeFlag == "near-image" {
		runNearImageMode(root, origin, likely, *workers, *threshold, *deleteMode, *trashMode, *deleteEmptyDirs, *jsonMode, *csvMode, sizeMap, seenDirs, started)
		return
	}

	var sizeGroupCandidates int64
	for _, grp := range sizeMap {
		if len(grp) >= 2 {
			sizeGroupCandidates += int64(len(grp))
		}
	}

	partialMap := make(map[string][]string)
	for _, grp := range sizeMap {
		if len(grp) < 2 {
			continue
		}
		for _, p := range grp {
			ph, err := hashing.Partial(p)
			if err != nil {
				continue
			}
			partialMap[ph] = append(partialMap[ph], p)
		}
	}

	candidateGroups := make(map[string][]string)
	for ph, paths := range partialMap {
		if len(paths) > 1 {
			candidateGroups[ph] = paths
		}
	}

	var partialSurvivors int64
	for _, grp := range candidateGroups {
		partialSurvivors += int64(len(grp))
	}

	pq := &PriorityQueue{}
	heap.Init(pq)
	for _, grp := range candidateGroups {
		for _, p := range grp {
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			heap.Push(pq, &FileJob{path: p, size: info.Size()})
		}
	}

	jobs := make(chan *FileJob)
	results := make(chan result)
	var wg sync.WaitGroup

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				var h string
				var err error
				if dbEnabled {
					if existing, ok := cache.TryDB(db, job.path); ok {
						h = existing
					} else {
						h, err = hashing.Full(job.path, *mmapFlag, *hashAlgo)
						if err == nil {
							cache.UpdateDB(db, job.path, h)
						}
					}
				} else {
					h, err = hashing.Full(job.path, *mmapFlag, *hashAlgo)
				}
				results <- result{
					path: job.path,
					hash: h,
					size: job.size,
					err:  err,
				}
			}
		}()
	}

	go func() {
		for pq.Len() > 0 {
			job := heap.Pop(pq).(*FileJob)
			jobs <- job
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	hashGroups := make(map[string][]string)
	sizeByPath := make(map[string]int64)
	for res := range results {
		if res.err != nil {
			continue
		}
		hashGroups[res.hash] = append(hashGroups[res.hash], res.path)
		sizeByPath[res.path] = res.size
	}

	var fullSurvivors int64
	for _, paths := range hashGroups {
		if len(paths) >= 2 {
			fullSurvivors += int64(len(paths))
		}
	}

	outName := "dryrun_duplicates.txt"
	if *deleteMode {
		outName = "duplicates.txt"
	}
	skipName := "skipped_duplicates.txt"

	fOut, err := os.Create(outName)
	if err != nil {
		log.Fatalf("failed to create output file: %v", err)
	}
	defer fOut.Close()

	fSkip, err := os.Create(skipName)
	if err != nil {
		log.Fatalf("failed to create skipped file: %v", err)
	}
	defer fSkip.Close()

	type csvRecord struct {
		groupID int64
		hash    string
		sizeB   int64
		action  string
		path    string
	}
	var csvRecords []csvRecord

	var duplicateGroups int64
	var removableFiles int64
	var removableBytes int64
	dirDupCount := make(map[string]int)
	var jsonGroups []dupGroupJSON

	for _, group := range hashGroups {
		if len(group) < 2 {
			continue
		}
		duplicateGroups++

		preferred := rules.SortByPreference(group)
		toKeep, toDelete := rules.SelectKeepDelete(preferred, origin, likely)

		toDelete = rules.SortByPreference(toDelete)

		if origin != "" {
			hasOrigin := false
			for _, p := range group {
				if rules.IsUnder(p, origin) {
					hasOrigin = true
					break
				}
			}
			if hasOrigin {
				fmt.Fprintf(fSkip, "# Duplicate %d\n", duplicateGroups)
				for _, p := range toKeep {
					fmt.Fprintf(fSkip, "KEEP: %s\n", p)
				}
				for _, p := range toDelete {
					fmt.Fprintf(fSkip, "DEL:  %s\n", p)
				}
			}
		}

		var groupHash string
		for h, paths := range hashGroups {
			for _, p := range paths {
				if p == group[0] {
					groupHash = h
					break
				}
			}
			if groupHash != "" {
				break
			}
		}
		var groupSize int64
		if len(group) > 0 {
			groupSize = sizeByPath[group[0]]
		}
		if *jsonMode {
			jsonGroups = append(jsonGroups, dupGroupJSON{
				Hash:      groupHash,
				SizeBytes: groupSize,
				Keep:      toKeep,
				Delete:    toDelete,
			})
		}
		if *csvMode {
			for _, p := range toKeep {
				csvRecords = append(csvRecords, csvRecord{duplicateGroups, groupHash, sizeByPath[p], "KEEP", p})
			}
			for _, p := range toDelete {
				csvRecords = append(csvRecords, csvRecord{duplicateGroups, groupHash, sizeByPath[p], "DELETE", p})
			}
		}

		for _, p := range toDelete {
			removableFiles++
			removableBytes += sizeByPath[p]
			dirDupCount[filepath.Dir(p)]++
			fmt.Fprintln(fOut, p)
			fmt.Println(p)
			if *deleteMode {
				_ = os.Remove(p)
			} else if *trashMode {
				_ = trashFile(p)
			}
		}
	}

	elapsed := time.Since(started)
	fmt.Printf("Scanned           : %d files in %s\n", scannedFiles, elapsed)
	if walkErrors > 0 {
		fmt.Printf("Walk errors       : %d (permission denied etc.)\n", walkErrors)
	}
	if dotUnderscoreSkipped > 0 {
		fmt.Printf("._* skipped       : %d\n", dotUnderscoreSkipped)
	}
	fmt.Printf("\nPipeline stats:\n")
	fmt.Printf("  Size candidates  : %d files (same size as ≥1 other)\n", sizeGroupCandidates)
	fmt.Printf("  After partial    : %d remain, %d filtered out\n", partialSurvivors, sizeGroupCandidates-partialSurvivors)
	fmt.Printf("  After full hash  : %d confirmed duplicates, %d filtered out\n", fullSurvivors, partialSurvivors-fullSurvivors)
	fmt.Printf("\nResults:\n")
	fmt.Printf("  Duplicate groups : %d\n", duplicateGroups)
	fmt.Printf("  Removable files  : %d\n", removableFiles)
	fmt.Printf("  Potential freed  : %.1f GiB\n", float64(removableBytes)/(1024*1024*1024))

	type dirCount struct {
		dir   string
		count int
	}
	var dcs []dirCount
	for d, c := range dirDupCount {
		dcs = append(dcs, dirCount{d, c})
	}
	sort.Slice(dcs, func(i, j int) bool { return dcs[i].count > dcs[j].count })
	const topN = 10
	if len(dcs) > 0 {
		fmt.Printf("\nTop directories by removable duplicates:\n")
		for i, dc := range dcs {
			if i >= topN {
				break
			}
			fmt.Printf("  %4d  %s\n", dc.count, dc.dir)
		}
	}

	if (*deleteMode || *trashMode) && *deleteEmptyDirs {
		var dirs []string
		for d := range seenDirs {
			dirs = append(dirs, d)
		}
		sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
		var removedDirs int64
		for _, d := range dirs {
			if d == root {
				continue
			}
			if entries, err := os.ReadDir(d); err == nil && len(entries) == 0 {
				if os.Remove(d) == nil {
					removedDirs++
				}
			}
		}
		if removedDirs > 0 {
			fmt.Printf("  Empty dirs removed: %d\n", removedDirs)
		}
	}

	if *csvMode && len(csvRecords) > 0 {
		if cf, err := os.Create("report.csv"); err == nil {
			w := csv.NewWriter(cf)
			_ = w.Write([]string{"group_id", "mode", "hash", "size_bytes", "action", "path"})
			for _, r := range csvRecords {
				_ = w.Write([]string{
					strconv.FormatInt(r.groupID, 10), "exact", r.hash,
					strconv.FormatInt(r.sizeB, 10), r.action, r.path,
				})
			}
			w.Flush()
			_ = cf.Close()
			fmt.Printf("\nCSV report written to report.csv\n")
		}
	}

	if dbEnabled {
		_, _ = db.Exec("UPDATE scans SET status='completed', completed_at=? WHERE id=?",
			time.Now().UnixNano(), activeScanID)
	}

	if *jsonMode {
		var topDirs []dirStatJSON
		for i, dc := range dcs {
			if i >= topN {
				break
			}
			topDirs = append(topDirs, dirStatJSON{Dir: dc.dir, RemovableFiles: dc.count})
		}
		rep := reportJSON{
			Scanned:              scannedFiles,
			ElapsedSeconds:       elapsed.Seconds(),
			WalkErrors:           walkErrors,
			DotUnderscoreSkipped: dotUnderscoreSkipped,
			Pipeline: pipelineJSON{
				SizeCandidates: sizeGroupCandidates,
				AfterPartial:   partialSurvivors,
				AfterFullHash:  fullSurvivors,
			},
			DuplicateGroups: duplicateGroups,
			RemovableFiles:  removableFiles,
			RemovableBytes:  removableBytes,
			TopDirs:         topDirs,
			Groups:          jsonGroups,
		}
		if jb, err := json.MarshalIndent(rep, "", "  "); err == nil {
			_ = os.WriteFile("report.json", jb, 0o644)
			fmt.Printf("\nJSON report written to report.json\n")
		}
	}
}
