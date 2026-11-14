package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// leftover is a file a rollback left parked under a temporary name, either
// because moving it back failed or because its source name is now taken.
type leftover struct {
	temp   string
	source string
}

// applyError is an apply failure plus the files that could not be put back.
type applyError struct {
	err      error
	leftover []leftover
}

func (e *applyError) Error() string {
	if len(e.leftover) == 0 {
		return e.err.Error()
	}
	var b strings.Builder
	b.WriteString(e.err.Error())
	b.WriteString(" (not restored:")
	for _, l := range e.leftover {
		fmt.Fprintf(&b, " %s is at %s;", l.source, l.temp)
	}
	b.WriteString(" move it back by hand)")
	return b.String()
}

func (e *applyError) Unwrap() error { return e.err }

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
		tmp, err := tempName(dp.dir)
		if err == nil {
			if err = os.Rename(r.Source, tmp); err != nil {
				os.Remove(tmp) // drop the empty placeholder; the source is untouched
			}
		}
		if err != nil {
			return nil, &applyError{err: err, leftover: rollback(dp.renames[:i], staged[:i])}
		}
		staged[i] = tmp
	}

	for i, r := range dp.renames {
		if err := os.Rename(staged[i], r.Target); err != nil {
			left := rollback(dp.renames[i:], staged[i:])
			return dp.renames[:i], &applyError{err: err, leftover: left}
		}
		staged[i] = ""
	}
	return dp.renames, nil
}

// rollback moves staged files back to their sources, last first. A source that
// is occupied is never overwritten: the file is reported as a leftover instead,
// so both files survive and the user can finish the move by hand.
func rollback(rs []rename, staged []string) []leftover {
	var left []leftover
	for i := len(rs) - 1; i >= 0; i-- {
		if staged[i] == "" {
			continue
		}
		if _, err := os.Lstat(rs[i].Source); !errors.Is(err, os.ErrNotExist) {
			left = append(left, leftover{temp: staged[i], source: rs[i].Source})
			continue
		}
		if err := os.Rename(staged[i], rs[i].Source); err != nil {
			left = append(left, leftover{temp: staged[i], source: rs[i].Source})
		}
	}
	return left
}

// tempName creates a hidden placeholder in dir and returns its path. The caller
// renames a source onto the placeholder, so the name is never free for another
// writer to take between choosing it and using it.
func tempName(dir string) (string, error) {
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
