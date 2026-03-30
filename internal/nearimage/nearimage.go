// Package nearimage provides perceptual-hash-based near-duplicate detection for images.
// It supports dHash + pHash computation, EXIF metadata extraction, filename-family
// detection, and union-find grouping by Hamming distance.
package nearimage

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/corona10/goimagehash"
	"github.com/rwcarlsen/goexif/exif"
)

// SupportedExts is the set of extensions processed in near-image mode.
var SupportedExts = map[string]struct{}{
	"jpg": {}, "jpeg": {}, "png": {}, "gif": {},
	"tif": {}, "tiff": {},
}

// IsImagePath returns true when path has a supported image extension.
func IsImagePath(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	_, ok := SupportedExts[ext]
	return ok
}

// ImageInfo holds perceptual hashes and metadata for a single image file.
type ImageInfo struct {
	Path     string
	DHash    uint64
	PHash    uint64
	Width    int
	Height   int
	SizeB    int64
	DateTime time.Time
	Camera   string
	HasEXIF  bool
}

// Compute opens path, decodes the image, computes dHash + pHash, and reads EXIF data.
// Returns an error if the file cannot be decoded as an image.
func Compute(path string) (*ImageInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	dh, err := goimagehash.DifferenceHash(img)
	if err != nil {
		return nil, err
	}
	ph, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return nil, err
	}

	b := img.Bounds()
	img = nil // release decoded bitmap; GC can reclaim it before EXIF parsing

	st, _ := f.Stat()
	var sizeB int64
	if st != nil {
		sizeB = st.Size()
	}

	info := &ImageInfo{
		Path:   path,
		DHash:  dh.GetHash(),
		PHash:  ph.GetHash(),
		Width:  b.Max.X - b.Min.X,
		Height: b.Max.Y - b.Min.Y,
		SizeB:  sizeB,
	}

	if _, err := f.Seek(0, io.SeekStart); err == nil {
		if x, err := exif.Decode(f); err == nil {
			info.HasEXIF = true
			if dt, err := x.DateTime(); err == nil {
				info.DateTime = dt
			}
			var parts []string
			if m, err := x.Get(exif.Make); err == nil {
				if sv, err2 := m.StringVal(); err2 == nil {
					if s := strings.TrimRight(sv, "\x00 "); s != "" {
						parts = append(parts, s)
					}
				}
			}
			if m, err := x.Get(exif.Model); err == nil {
				if sv, err2 := m.StringVal(); err2 == nil {
					if s := strings.TrimRight(sv, "\x00 "); s != "" {
						parts = append(parts, s)
					}
				}
			}
			info.Camera = strings.Join(parts, " ")
		}
	}

	return info, nil
}

// HammingDistance returns the number of differing bits between two 64-bit hashes.
func HammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// familyRe strips common copy/edit suffixes so that "IMG_1234-edited.jpg" and
// "IMG_1234(1).jpg" both resolve to the family name "IMG_1234".
var familyRe = regexp.MustCompile(`(?i)[-_ (]+(edited|filtered|copy|edit|bearbeitet|\d+)\)?$|[-_]\d+$`)

// FileFamily extracts the base "family name" from a file path, stripping trailing
// copy/edit/number suffixes.
func FileFamily(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = familyRe.ReplaceAllString(name, "")
	return strings.TrimRight(name, "-_ ")
}

// PreferDelete returns true if a should be deleted in preference to b.
// Priority: lower resolution → delete first; then filename-pattern heuristics;
// then newer EXIF timestamp (likely a processed export, not the original).
func PreferDelete(a, b *ImageInfo) bool {
	aPixels := a.Width * a.Height
	bPixels := b.Width * b.Height
	if aPixels != bPixels {
		return aPixels < bPixels
	}
	if a.SizeB != b.SizeB {
		return a.SizeB < b.SizeB
	}
	if a.HasEXIF && b.HasEXIF && !a.DateTime.IsZero() && !b.DateTime.IsZero() {
		if !a.DateTime.Equal(b.DateTime) {
			return a.DateTime.After(b.DateTime)
		}
	}
	return false
}

// SortByPreference returns a copy of imgs sorted so that the least-preferred
// (most delete-worthy) image is first.
func SortByPreference(imgs []*ImageInfo) []*ImageInfo {
	sorted := make([]*ImageInfo, len(imgs))
	copy(sorted, imgs)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && PreferDelete(sorted[j-1], sorted[j]); j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	return sorted
}

// unionFind is a path-compressed disjoint-set structure.
type unionFind struct {
	parent []int
}

func newUF(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

func (uf *unionFind) find(x int) int {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]]
		x = uf.parent[x]
	}
	return x
}

func (uf *unionFind) union(x, y int) {
	rx, ry := uf.find(x), uf.find(y)
	if rx != ry {
		uf.parent[rx] = ry
	}
}

// GroupBySimilarity groups images whose dHash Hamming distance is ≤ threshold.
// Only groups with ≥ 2 members are returned.
// Time complexity: O(n²) — suitable for photo collections up to ~50 k images.
func GroupBySimilarity(images []*ImageInfo, threshold int) [][]*ImageInfo {
	n := len(images)
	if n == 0 {
		return nil
	}
	uf := newUF(n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if HammingDistance(images[i].DHash, images[j].DHash) <= threshold {
				uf.union(i, j)
			}
		}
	}

	groupMap := make(map[int][]*ImageInfo)
	for i, img := range images {
		r := uf.find(i)
		groupMap[r] = append(groupMap[r], img)
	}

	var result [][]*ImageInfo
	for _, g := range groupMap {
		if len(g) >= 2 {
			result = append(result, g)
		}
	}
	return result
}
