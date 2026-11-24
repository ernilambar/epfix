package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// applyDir moves a plan in two phases: every source to a unique temporary
// name, then every temporary to its target. This makes chains and case-only
// renames safe. On a mid-flight error the completed prefix stays applied and
// the rest is moved back to its source.
//
// A rollback that cannot complete is part of the returned error, naming the
// temporary path each stranded file is left at, so a failed move never loses
// track of a file.
func applyDir(dp dirPlan) ([]rename, error) {
	if len(dp.renames) == 0 {
		return nil, nil
	}

	staged := make([]string, len(dp.renames))
	for i, r := range dp.renames {
		tmp, err := tempName(dp.dir, dp.entriesAreDirs)
		if err == nil {
			if err = os.Rename(r.Source, tmp); err != nil {
				// Drop the empty file placeholder; the source is untouched. A
				// directory placeholder was already removed, and removing the
				// name now could delete whatever won the race for it.
				if !dp.entriesAreDirs {
					os.Remove(tmp)
				}
			}
		}
		if err != nil {
			return nil, rollback(err, dp.renames[:i], staged[:i])
		}
		staged[i] = tmp
	}

	for i, r := range dp.renames {
		if err := os.Rename(staged[i], r.Target); err != nil {
			return dp.renames[:i], rollback(err, dp.renames[i:], staged[i:])
		}
		staged[i] = ""
	}
	return dp.renames, nil
}

// rollback moves staged files back to their sources, last first, and returns
// err. A source that is occupied is never overwritten: os.Rename would replace
// the occupant, so the file stays at its temporary name and err names it.
func rollback(err error, rs []rename, staged []string) error {
	var stranded []string
	for i := len(rs) - 1; i >= 0; i-- {
		if staged[i] == "" {
			continue
		}
		if _, statErr := os.Lstat(rs[i].Source); errors.Is(statErr, os.ErrNotExist) && os.Rename(staged[i], rs[i].Source) == nil {
			continue
		}
		stranded = append(stranded, fmt.Sprintf("%s is at %s", rs[i].Source, staged[i]))
	}
	if len(stranded) == 0 {
		return err
	}
	return fmt.Errorf("%w (not restored: %s; move it back by hand)", err, strings.Join(stranded, "; "))
}

// applyDirRenames applies folder renames in batches of siblings, deepest
// first, so renaming a parent folder never invalidates a child's planned path.
// Each batch is an applyDir plan, which stages same-directory chains.
func applyDirRenames(renames []rename) ([]rename, error) {
	if len(renames) == 0 {
		return nil, nil
	}
	groups := make(map[string][]rename, len(renames))
	var parents []string
	for _, r := range renames {
		p := filepath.Dir(r.Source)
		if _, ok := groups[p]; !ok {
			parents = append(parents, p)
		}
		groups[p] = append(groups[p], r)
	}
	sortParents(parents)
	var applied []rename
	for _, p := range parents {
		done, err := applyDir(dirPlan{dir: p, renames: groups[p], entriesAreDirs: true})
		applied = append(applied, done...)
		if err != nil {
			return applied, err
		}
	}
	return applied, nil
}

// sortParents orders source parents deepest first, so a rename below another
// planned rename never works with a stale path.
func sortParents(parents []string) {
	sort.Slice(parents, func(i, j int) bool {
		if di, dj := pathDepth(parents[i]), pathDepth(parents[j]); di != dj {
			return di > dj
		}
		return parents[i] < parents[j]
	})
}

func pathDepth(p string) int {
	abs, err := filepath.Abs(p)
	if err != nil {
		return strings.Count(p, string(filepath.Separator))
	}
	return strings.Count(abs, string(filepath.Separator))
}

// tempName creates a hidden placeholder in dir and returns its path. The caller
// renames a source onto the placeholder, so the name is never free for another
// writer to take between choosing it and using it. macOS refuses to rename a
// directory onto an existing path, so a directory placeholder only reserves a
// unique name: it is removed before the caller moves the source onto it.
func tempName(dir string, isDir bool) (string, error) {
	if isDir {
		tmp, err := os.MkdirTemp(dir, ".epfix-tmp-*")
		if err != nil {
			return "", err
		}
		return tmp, os.Remove(tmp)
	}
	f, err := os.CreateTemp(dir, ".epfix-tmp-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}
