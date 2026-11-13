package main

import "os"

// applyDir moves a plan in two phases: every source to a unique temporary
// name, then every temporary to its target. This makes chains and case-only
// renames safe. On a mid-flight error the completed prefix stays applied and
// the rest is moved back to its source.
func applyDir(dp dirPlan) ([]rename, error) {
	if len(dp.renames) == 0 {
		return nil, nil
	}

	staged := make([]string, len(dp.renames))
	for i, r := range dp.renames {
		tmp, err := tempName(dp.dir)
		if err != nil {
			restore(dp.renames[:i], staged[:i])
			return nil, err
		}
		if err := os.Rename(r.Source, tmp); err != nil {
			os.Remove(tmp)
			restore(dp.renames[:i], staged[:i])
			return nil, err
		}
		staged[i] = tmp
	}

	done := 0
	var failure error
	for i, r := range dp.renames {
		if err := os.Rename(staged[i], r.Target); err != nil {
			failure = err
			break
		}
		staged[i] = ""
		done++
	}
	if failure != nil {
		restore(dp.renames[done:], staged[done:])
		return dp.renames[:done], failure
	}
	return dp.renames, nil
}

func restore(rs []rename, staged []string) {
	for i, r := range rs {
		if staged[i] != "" {
			os.Rename(staged[i], r.Source)
		}
	}
}

func tempName(dir string) (string, error) {
	f, err := os.CreateTemp(dir, ".epfix-tmp-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}
