package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Show.S04E01.1080p.mkv",
		"Show.S04E01.1080p.en.srt",
		"Show.S04E02.720p.mkv",
	} {
		writeFile(t, dir, name)
	}

	dp, err := planDir(dir, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	applied, err := applyDir(dp)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 {
		t.Fatalf("want 3 applied, got %d", len(applied))
	}

	want := []string{"Show - S04E01.en.srt", "Show - S04E01.mkv", "Show - S04E02.mkv"}
	got := names(t, dir)
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
	body, err := os.ReadFile(filepath.Join(dir, "Show - S04E01.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "Show.S04E01.1080p.mkv" {
		t.Fatalf("content not preserved: %q", body)
	}
}

// TestApplySwap proves the two-phase move: a→b and b→a in one batch. This
// cannot arise from detection (library names are fixed points), but the apply
// machinery must be correct for it regardless.
func TestApplySwap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt")
	writeFile(t, dir, "b.txt")

	// Overwrite contents so the swap is observable.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	dp := dirPlan{dir: dir, renames: []rename{
		{Source: filepath.Join(dir, "a.txt"), Target: filepath.Join(dir, "b.txt")},
		{Source: filepath.Join(dir, "b.txt"), Target: filepath.Join(dir, "a.txt")},
	}}
	if _, err := applyDir(dp); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a.txt": "B", "b.txt": "A"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != want {
			t.Fatalf("%s: want %q, got %q", name, want, body)
		}
	}
}

func TestCaseOnlyRename(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Show Name - S04E01.mkv")

	o := testOptions()
	o.show = "show name"
	dp, err := planDir(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(dp.conflicts) != 0 || len(dp.renames) != 1 {
		t.Fatalf("want one rename, got %+v %+v", dp.renames, dp.conflicts)
	}
	if _, err := applyDir(dp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "show name - S04E01.mkv")); err != nil {
		t.Fatalf("case-only rename failed: %v", err)
	}
}
