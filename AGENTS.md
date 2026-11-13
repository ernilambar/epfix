# AGENTS.md

## Overview

epfix is a Go CLI that renames TV episode files from release names
(`Show.S04E01.1080p.mkv`) to library names (`Show - S04E01.mkv`) in place. It
prints a plan by default and renames only with `--apply`. Standard library
only, one `main` package in the repository root.

## Setup

Go 1.22 or newer (`go.mod`). No third-party modules, no environment variables,
no config files, no network access.

```sh
go mod download
go build -o epfix .
```

## Commands

```sh
go build -o epfix .                     # build
go test ./...                           # test
go vet ./...                            # lint
gofmt -w .                              # format
go test -run='^$' ./...                 # typecheck (compiles package and tests)
go test -run TestGoldenNames -v ./...   # focused: the golden name table
```

`.github/workflows/lint.yml` runs the same format/vet/test/build checks on
`main` and on pull requests. `.github/workflows/release.yml` publishes a single
raw binary, `epfix-darwin-arm64` (`main.version` stamped from the tag), for a
`v*` tag. macOS on Apple silicon is the only supported platform: do not add an
amd64 build. Packaging and distribution are handled outside this repository:
no formula or packaging config belongs here; `README.md` documents installation
only.

## Releasing

The version is never edited in source: `main.version` defaults to `dev` and the
git tag supplies the release version (`-X main.version=${GITHUB_REF_NAME#v}` in
`release.yml`). Cutting a release touches two files:

1. `CHANGELOG.md` — add `## X.Y.Z - YYYY-MM-DD` plus bullets at the top, commit.
2. The tag — `git tag vX.Y.Z && git push origin vX.Y.Z` builds and publishes
   `epfix-darwin-arm64`.

Packaging is a separate concern handled outside this repository. Do not
hard-code a version in `main.go`, `README.md`, or a badge: the tag and the
`CHANGELOG.md` entry are the only places a release version is written.

## Conventions

1. **Standard library only.** Do not add dependencies, subpackages, or new
   directories; new code belongs in `main.go`, `plan.go`, or `apply.go`.
2. **Planning is read-only.** `planDir`, `planPaths`, `detect`, and
   `renderTemplate` must never touch the filesystem. Only `applyDir` writes,
   and only after `runPlan` has confirmed `opt.apply` and zero conflicts.
3. **Conflicts abort the whole invocation.** If any `dirPlan.conflicts` is
   non-empty, no file may move, and the exit code is `1`. Conflict kinds:
   duplicate target (case-insensitive, per directory), an existing target that
   is not itself a rename source, and empty or `/`/`:`-containing output names.
4. **All renames go through `applyDir`.** It stages every source to a temporary
   name, then moves each temporary to its target, so chains and case-only
   renames cannot collide. Do not call `os.Rename(source, target)` directly
   from a new code path.
5. **The file-name language is fixed and lives in `plan.go`.** Detection is the
   two regexes (`SxxEyy`/`SxxEyyEzz`, then `1x02`) plus `normalizeShow`; templates
   accept only `{show}`, `{season}`, `{episode}`; videos are classified solely by
   `options.exts`, and sidecars solely by video stem (never by extension). Never
   special-case an individual filename — extend the table in `plan_test.go`
   instead, and do not relax existing expected names.

## Quality gate

Run from the repository root in this order. Every command must exit `0`, and
`gofmt -l .` must print nothing (a dirty format still exits `0`).

```sh
gofmt -l .
go vet ./...
go test ./...
go build -o /tmp/epfix-check .
```

Report the commands you ran. Do not claim completion if any step fails; fix the
cause instead of loosening a test.
