package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/effem/duplicate_finder/internal/nearimage"
	"github.com/effem/duplicate_finder/internal/rules"
)

// nearGroupJSON is the JSON representation of one near-image similarity group.
type nearGroupJSON struct {
	MinDHashDist int      `json:"min_dhash_dist"`
	SizeBytes    int64    `json:"size_bytes"`
	Keep         []string `json:"keep"`
	Delete       []string `json:"delete"`
}

// nearReportJSON is the top-level JSON structure for near-image mode.
type nearReportJSON struct {
	Scanned        int64           `json:"scanned"`
	ElapsedSeconds float64         `json:"elapsed_s"`
	Threshold      int             `json:"threshold"`
	SimilarGroups  int64           `json:"similar_groups"`
	RemovableFiles int64           `json:"removable_files"`
	RemovableBytes int64           `json:"removable_bytes"`
	TopDirs        []dirStatJSON   `json:"top_dirs,omitempty"`
	Groups         []nearGroupJSON `json:"groups"`
}

// runNearImageMode is the entry point for -mode near-image.
// It receives the already-walked sizeMap (all candidate files) and seenDirs
// so the walk is not repeated.
func runNearImageMode(
	root, origin, likely string,
	workers, threshold int,
	deleteMode, trashMode, deleteEmptyDirs, jsonMode, csvMode bool,
	sizeMap map[int64][]string,
	seenDirs map[string]struct{},
	started time.Time,
) {
	// Collect all files from sizeMap into a flat slice.
	var allPaths []string
	for _, paths := range sizeMap {
		allPaths = append(allPaths, paths...)
	}

	fmt.Printf("Near-image mode: computing perceptual hashes for %d files (threshold=%d)...\n",
		len(allPaths), threshold)

	// Filter to image paths only.
	var imagePaths []string
	for _, p := range allPaths {
		if nearimage.IsImagePath(p) {
			imagePaths = append(imagePaths, p)
		}
	}
	fmt.Printf("Image files found: %d\n", len(imagePaths))

	if len(imagePaths) == 0 {
		fmt.Println("No image files to process.")
		return
	}

	// Compute perceptual hashes in parallel.
	type hashResult struct {
		info *nearimage.ImageInfo
		err  error
	}
	jobs := make(chan string, workers*2)
	results := make(chan hashResult, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				info, err := nearimage.Compute(p)
				results <- hashResult{info, err}
			}
		}()
	}
	go func() {
		for _, p := range imagePaths {
			jobs <- p
		}
		close(jobs)
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var images []*nearimage.ImageInfo
	var hashErrors int
	for r := range results {
		if r.err != nil {
			hashErrors++
			continue
		}
		images = append(images, r.info)
	}
	if hashErrors > 0 {
		fmt.Printf("Hash errors (unsupported/corrupt images): %d\n", hashErrors)
	}

	// Group by dHash similarity.
	groups := nearimage.GroupBySimilarity(images, threshold)
	fmt.Printf("Similarity groups  : %d\n", len(groups))

	outName := "dryrun_near_duplicates.txt"
	if deleteMode || trashMode {
		outName = "near_duplicates.txt"
	}
	fOut, err := os.Create(outName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output file: %v\n", err)
		return
	}
	defer fOut.Close()

	var (
		dupGroups      int64
		removableFiles int64
		removableBytes int64
	)
	dirDupCount := make(map[string]int)
	var jsonGroups []nearGroupJSON

	type csvRec struct {
		groupID int64
		dist    int
		sizeB   int64
		action  string
		path    string
	}
	var csvRecords []csvRec

	for _, group := range groups {
		dupGroups++

		// Sort: most delete-worthy first.
		sorted := nearimage.SortByPreference(group)

		// Respect origin/likely_duplicates path policies.
		var toKeep, toDelete []*nearimage.ImageInfo
		if origin != "" || likely != "" {
			var keepPaths, deletePaths []string
			for _, img := range sorted {
				keepPaths = append(keepPaths, img.Path)
			}
			keepPaths, deletePaths = rules.SelectKeepDelete(keepPaths, origin, likely)
			keepSet := make(map[string]struct{}, len(keepPaths))
			for _, p := range keepPaths {
				keepSet[p] = struct{}{}
			}
			for _, img := range sorted {
				if _, ok := keepSet[img.Path]; ok {
					toKeep = append(toKeep, img)
				} else {
					toDelete = append(toDelete, img)
				}
			}
			_ = deletePaths
		} else {
			// No path policy: keep first (highest quality per SortByPreference), delete rest.
			toKeep = sorted[:1]
			toDelete = sorted[1:]
		}

		// Compute minimum Hamming distance within this group for reporting.
		minDist := 64
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				if d := nearimage.HammingDistance(group[i].DHash, group[j].DHash); d < minDist {
					minDist = d
				}
			}
		}

		if jsonMode {
			var keepPaths, deletePaths []string
			for _, img := range toKeep {
				keepPaths = append(keepPaths, img.Path)
			}
			for _, img := range toDelete {
				deletePaths = append(deletePaths, img.Path)
			}
			var repSize int64
			if len(toKeep) > 0 {
				repSize = toKeep[0].SizeB
			}
			jsonGroups = append(jsonGroups, nearGroupJSON{
				MinDHashDist: minDist,
				SizeBytes:    repSize,
				Keep:         keepPaths,
				Delete:       deletePaths,
			})
		}

		header := fmt.Sprintf("--- Group %d  dist:%d  %d files ---", dupGroups, minDist, len(group))
		fmt.Fprintln(fOut, header)
		fmt.Println(header)

		for _, img := range toKeep {
			line := fmt.Sprintf("  KEEP  %s  (%dx%d, %s)", img.Path, img.Width, img.Height, formatBytes(img.SizeB))
			fmt.Fprintln(fOut, line)
			fmt.Println(line)
			if csvMode {
				csvRecords = append(csvRecords, csvRec{dupGroups, minDist, img.SizeB, "KEEP", img.Path})
			}
		}

		for _, img := range toDelete {
			removableFiles++
			removableBytes += img.SizeB
			dirDupCount[filepath.Dir(img.Path)]++
			line := fmt.Sprintf("  DEL   %s  (%dx%d, %s)", img.Path, img.Width, img.Height, formatBytes(img.SizeB))
			fmt.Fprintln(fOut, line)
			fmt.Println(line)
			if csvMode {
				csvRecords = append(csvRecords, csvRec{dupGroups, minDist, img.SizeB, "DELETE", img.Path})
			}
			if deleteMode {
				_ = os.Remove(img.Path)
			} else if trashMode {
				_ = trashFile(img.Path)
			}
		}
	}

	elapsed := time.Since(started)
	fmt.Printf("\nScanned           : %d image files in %s\n", len(images), elapsed)
	fmt.Printf("\nResults:\n")
	fmt.Printf("  Similar groups   : %d\n", dupGroups)
	fmt.Printf("  Removable files  : %d\n", removableFiles)
	fmt.Printf("  Potential freed  : %.1f GiB\n", float64(removableBytes)/(1024*1024*1024))

	// Top directories.
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
		fmt.Printf("\nTop directories by removable near-duplicates:\n")
		for i, dc := range dcs {
			if i >= topN {
				break
			}
			fmt.Printf("  %4d  %s\n", dc.count, dc.dir)
		}
	}

	// delete-empty-dirs
	if (deleteMode || trashMode) && deleteEmptyDirs {
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

	// CSV report.
	if csvMode && len(csvRecords) > 0 {
		if cf, err := os.Create("report_near.csv"); err == nil {
			w := csv.NewWriter(cf)
			_ = w.Write([]string{"group_id", "mode", "min_dhash_dist", "size_bytes", "action", "path"})
			for _, r := range csvRecords {
				_ = w.Write([]string{
					strconv.FormatInt(r.groupID, 10), "near-image",
					strconv.Itoa(r.dist),
					strconv.FormatInt(r.sizeB, 10), r.action, r.path,
				})
			}
			w.Flush()
			_ = cf.Close()
			fmt.Printf("\nCSV report written to report_near.csv\n")
		}
	}

	// JSON report.
	if jsonMode {
		var topDirs []dirStatJSON
		for i, dc := range dcs {
			if i >= topN {
				break
			}
			topDirs = append(topDirs, dirStatJSON{Dir: dc.dir, RemovableFiles: dc.count})
		}
		rep := nearReportJSON{
			Scanned:        int64(len(images)),
			ElapsedSeconds: elapsed.Seconds(),
			Threshold:      threshold,
			SimilarGroups:  dupGroups,
			RemovableFiles: removableFiles,
			RemovableBytes: removableBytes,
			TopDirs:        topDirs,
			Groups:         jsonGroups,
		}
		if jb, err := json.MarshalIndent(rep, "", "  "); err == nil {
			_ = os.WriteFile("report_near.json", jb, 0o644)
			fmt.Printf("\nJSON report written to report_near.json\n")
		}
	}
}
