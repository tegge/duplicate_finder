package rules_test

import (
	"path/filepath"
	"testing"

	"github.com/effem/duplicate_finder/internal/rules"
)

func p(s string) string { return filepath.FromSlash(s) }

func TestSelectKeepDelete_NoPolicy(t *testing.T) {
	preferred := []string{p("/scan/a/file.jpg"), p("/scan/b/file.jpg"), p("/scan/c/file.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, nil, nil)
	if len(keep) != 1 || keep[0] != preferred[0] {
		t.Errorf("keep: want [%s], got %v", preferred[0], keep)
	}
	if len(del) != 2 {
		t.Errorf("del: want 2, got %v", del)
	}
}

func TestSelectKeepDelete_OriginOnly(t *testing.T) {
	origin := p("/scan/origin")
	preferred := []string{p("/scan/other/file.jpg"), p("/scan/origin/file.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, []string{origin}, nil)
	if len(keep) != 1 || keep[0] != p("/scan/origin/file.jpg") {
		t.Errorf("keep: want origin file, got %v", keep)
	}
	if len(del) != 1 || del[0] != p("/scan/other/file.jpg") {
		t.Errorf("del: want other file, got %v", del)
	}
}

func TestSelectKeepDelete_LikelyOnly(t *testing.T) {
	likely := p("/scan/backup")
	preferred := []string{p("/scan/backup/file.jpg"), p("/scan/main/file.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, nil, []string{likely})
	if len(keep) != 1 || keep[0] != p("/scan/main/file.jpg") {
		t.Errorf("keep: want main file, got %v", keep)
	}
	if len(del) != 1 || del[0] != p("/scan/backup/file.jpg") {
		t.Errorf("del: want backup file, got %v", del)
	}
}

func TestSelectKeepDelete_BothPolicies(t *testing.T) {
	origin := p("/scan/origin")
	likely := p("/scan/backup")
	preferred := []string{p("/scan/origin/file.jpg"), p("/scan/backup/file.jpg"), p("/scan/other/file.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, []string{origin}, []string{likely})
	if len(keep) != 1 || keep[0] != p("/scan/origin/file.jpg") {
		t.Errorf("keep: want origin file, got %v", keep)
	}
	if len(del) != 2 {
		t.Errorf("del: want 2, got %v", del)
	}
}

func TestSelectKeepDelete_OriginMultiple(t *testing.T) {
	origin := p("/scan/origin")
	preferred := []string{p("/scan/origin/a.jpg"), p("/scan/origin/b.jpg"), p("/scan/other/c.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, []string{origin}, nil)
	if len(keep) != 2 {
		t.Errorf("keep: want 2 origin files, got %v", keep)
	}
	if len(del) != 1 || del[0] != p("/scan/other/c.jpg") {
		t.Errorf("del: want other file, got %v", del)
	}
}

func TestSelectKeepDelete_AllInLikely(t *testing.T) {
	likely := p("/scan/backup")
	preferred := []string{p("/scan/backup/a.jpg"), p("/scan/backup/b.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, nil, []string{likely})
	if len(keep) != 1 {
		t.Errorf("keep: want 1 when all in likely, got %v", keep)
	}
	if len(del) != 1 {
		t.Errorf("del: want 1, got %v", del)
	}
}

func TestSelectKeepDelete_NoOriginInGroup(t *testing.T) {
	origin := p("/scan/origin")
	preferred := []string{p("/scan/a/file.jpg"), p("/scan/b/file.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, []string{origin}, nil)
	if len(keep) != 1 || keep[0] != preferred[0] {
		t.Errorf("keep: want first file when no origin match, got %v", keep)
	}
	if len(del) != 1 {
		t.Errorf("del: want 1, got %v", del)
	}
}

func TestSortByPreference_PendingLast(t *testing.T) {
	paths := []string{
		p("/data/photo.jpg"),
		p("/data/.pending_photo.jpg"),
		p("/data/photo (1).jpg"),
	}
	sorted := rules.SortByPreference(paths)
	if sorted[0] != p("/data/photo.jpg") {
		t.Errorf("want plain file first (keep-worthy), got %s", sorted[0])
	}
	if sorted[len(sorted)-1] != p("/data/.pending_photo.jpg") {
		t.Errorf("want .pending_ last (delete-worthy), got %s", sorted[len(sorted)-1])
	}
}

func TestSortByPreference_CopyAfterNormal(t *testing.T) {
	paths := []string{
		p("/data/photo (2).jpg"),
		p("/data/photo.jpg"),
	}
	sorted := rules.SortByPreference(paths)
	if sorted[0] != p("/data/photo.jpg") {
		t.Errorf("want plain file first (keep-worthy), got %s", sorted[0])
	}
	if sorted[1] != p("/data/photo (2).jpg") {
		t.Errorf("want copy variant last (delete-worthy), got %s", sorted[1])
	}
}

func TestKeepDelete_CopyVariantDeleted(t *testing.T) {
	paths := []string{
		p("/data/photo (1).jpg"),
		p("/data/photo.jpg"),
	}
	sorted := rules.SortByPreference(paths)
	keep, del := rules.SelectKeepDelete(sorted, nil, nil)
	if len(keep) != 1 || keep[0] != p("/data/photo.jpg") {
		t.Errorf("keep: want plain file, got %v", keep)
	}
	if len(del) != 1 || del[0] != p("/data/photo (1).jpg") {
		t.Errorf("del: want copy variant, got %v", del)
	}
}

func TestSelectKeepDelete_MultiLikely(t *testing.T) {
	likelies := []string{p("/scan/backup"), p("/scan/old")}
	preferred := []string{p("/scan/main/file.jpg"), p("/scan/backup/file.jpg"), p("/scan/old/file.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, nil, likelies)
	if len(keep) != 1 || keep[0] != p("/scan/main/file.jpg") {
		t.Errorf("keep: want main file, got %v", keep)
	}
	if len(del) != 2 {
		t.Errorf("del: want 2, got %v", del)
	}
}

func TestSelectKeepDelete_MultiOrigin(t *testing.T) {
	origins := []string{p("/scan/origA"), p("/scan/origB")}
	preferred := []string{p("/scan/origA/file.jpg"), p("/scan/origB/file.jpg"), p("/scan/other/file.jpg")}
	keep, del := rules.SelectKeepDelete(preferred, origins, nil)
	if len(keep) != 2 {
		t.Errorf("keep: want 2 origin files, got %v", keep)
	}
	if len(del) != 1 || del[0] != p("/scan/other/file.jpg") {
		t.Errorf("del: want other file, got %v", del)
	}
}

func TestIsUnder(t *testing.T) {
	sep := string(filepath.Separator)
	base := p("/some/dir")
	child := p("/some/dir") + sep + "file.txt"
	if !rules.IsUnder(child, base) {
		t.Errorf("expected %s to be under %s", child, base)
	}
	if rules.IsUnder(p("/some/dir_extra/file.txt"), base) {
		t.Errorf("prefix collision: /some/dir_extra should not be under /some/dir")
	}
	if rules.IsUnder(base, base) {
		t.Errorf("dir itself should not be under itself")
	}
}
