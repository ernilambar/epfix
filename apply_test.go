package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	dp, err := planDir(dir, nil, testOptions())
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

// TestApplyRollback proves a failed move is undone: the source comes back and
// no temporary file is left behind.
func TestApplyRollback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory at the target makes the move fail after staging.
	if err := os.Mkdir(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	dp := dirPlan{dir: dir, renames: []rename{
		{Source: filepath.Join(dir, "a.txt"), Target: filepath.Join(dir, "taken")},
	}}
	applied, err := applyDir(dp)
	if err == nil {
		t.Fatal("want error")
	}
	if len(applied) != 0 {
		t.Fatalf("want nothing applied, got %d", len(applied))
	}
	if strings.Contains(err.Error(), "not restored") {
		t.Fatalf("rollback should have succeeded: %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(dir, "a.txt"))
	if readErr != nil {
		t.Fatalf("source not restored: %v", readErr)
	}
	if string(body) != "A" {
		t.Fatalf("content not preserved: %q", body)
	}
	if got := names(t, dir); len(got) != 2 || got[0] != "a.txt" || got[1] != "taken" {
		t.Fatalf("temporary file left behind: %v", got)
	}
}

// TestApplyRollbackLeftover proves a rolled-back file whose source name is
// taken is reported with its temporary path instead of overwriting the
// occupant or vanishing from the report.
func TestApplyRollbackLeftover(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"a.txt": "A", "b.txt": "B"} {
		writeFile(t, dir, name)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a→b succeeds, b→taken fails, and b is then occupied, so b's file cannot
	// be put back.
	dp := dirPlan{dir: dir, renames: []rename{
		{Source: filepath.Join(dir, "a.txt"), Target: filepath.Join(dir, "b.txt")},
		{Source: filepath.Join(dir, "b.txt"), Target: filepath.Join(dir, "taken")},
	}}
	applied, err := applyDir(dp)
	if err == nil {
		t.Fatal("want error")
	}
	if len(applied) != 1 {
		t.Fatalf("want one applied, got %d", len(applied))
	}
	msg := err.Error()
	if !strings.Contains(msg, "not restored") || !strings.Contains(msg, ".epfix-tmp-") || !strings.Contains(msg, "b.txt") {
		t.Fatalf("leftover not reported: %v", err)
	}
	// The occupant survives, and the stranded file is still readable at the
	// temporary path named in the message.
	body, readErr := os.ReadFile(filepath.Join(dir, "b.txt"))
	if readErr != nil || string(body) != "A" {
		t.Fatalf("occupant changed: %q %v", body, readErr)
	}
	tmp := msg[strings.Index(msg, ".epfix-tmp-"):]
	tmp = filepath.Join(dir, strings.Fields(tmp)[0])
	body, readErr = os.ReadFile(tmp)
	if readErr != nil || string(body) != "B" {
		t.Fatalf("stranded file unreadable: %q %v", body, readErr)
	}
}

// TestApplyMovieFolder proves the pipeline order: files are renamed inside the
// folder first, then the folder is renamed, leaving one canonical path.
func TestApplyMovieFolder(t *testing.T) {
	root := t.TempDir()
	const folder = "All We Imagine As Light (2024) [720p] [BluRay] [YTS.MX]"
	dir := filepath.Join(root, folder)
	writeFile(t, dir, "All.We.Imagine.As.Light.2024.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4")

	var out, errOut bytes.Buffer
	if code := run([]string{"--movies", "--apply", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	wantDir := filepath.Join(root, "All We Imagine As Light (2024)")
	body, err := os.ReadFile(filepath.Join(wantDir, "All We Imagine As Light (2024).mp4"))
	if err != nil {
		t.Fatalf("folder or file not renamed: %v", err)
	}
	if string(body) != "All.We.Imagine.As.Light.2024.720p.BluRay.x264.AAC5.1-[YTS.MX].mp4" {
		t.Fatalf("content not preserved: %q", body)
	}
	if got := names(t, root); len(got) != 1 || got[0] != filepath.Base(wantDir) {
		t.Fatalf("temporary entry or old folder left behind: %v", got)
	}
}

// TestApplyShowFolder proves a season pack is renamed in place after its files
// are renamed.
func TestApplyShowFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Show.Name.S04.1080p.WEB-DL-GROUP")
	writeFile(t, dir, "Show.Name.S04E01.1080p.WEB-DL-GROUP.mkv")

	var out, errOut bytes.Buffer
	if code := run([]string{"--apply", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	want := filepath.Join(root, "Show Name - S04", "Show Name - S04E01.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("season folder not renamed: %v", err)
	}
	if got := names(t, root); len(got) != 1 || got[0] != "Show Name - S04" {
		t.Fatalf("root: %v", got)
	}
}

// TestApplyShowFolderExistingShowTree leaves an existing show/season tree alone
// and renames the release folder beside it.
func TestApplyShowFolderExistingShowTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Show Name", "Season 01"), "Show.Name.S01E01.mkv")
	dir := filepath.Join(root, "Show.Name.S02.1080p")
	writeFile(t, dir, "Show.Name.S02E01.mkv")

	var out, errOut bytes.Buffer
	if code := run([]string{"--apply", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, "Show Name - S02", "Show Name - S02E01.mkv")); err != nil {
		t.Fatalf("season file not renamed into place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Show Name", "Season 01", "Show.Name.S01E01.mkv")); err != nil {
		t.Fatalf("existing show tree changed: %v", err)
	}
}

func TestCaseOnlyRename(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Show Name - S04E01.mkv")

	o := testOptions()
	o.title = "show name"
	dp, err := planDir(dir, nil, o)
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
