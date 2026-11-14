# AGENTS.md

## Overview

epfix is a Go CLI that renames TV episode files and, with `--movies`, movie
files and movie folders from release names to library names in place. A TV
season folder named on the command line is renamed
(`Show.Name.S04.1080p.WEB-DL/` → `Show Name - S04/`); inside its show folder it
becomes `Season 04`. Renames:
`Show.S04E01.1080p.mkv` → `Show - S04E01.mkv`,
`Movie.2026.1080p.mkv` → `Movie (2026).mkv`,
`Movie (2026) [1080p]/` → `Movie (2026)/`. It prints a plan by default and
renames only with `--apply`. Standard library only, one `main` package in the
repository root.

## Setup

Go 1.22 or newer (`go.mod`). No third-party modules, no environment variables,
no config files, no network access.

```sh
go mod download
go build -o epfix .
```

## Commands

```sh
go build -o epfix .                        # build
go test ./...                              # test
go vet ./...                               # lint
gofmt -w .                                 # format
go test -run='^$' ./...                    # typecheck (compiles package and tests)
go test -run TestGoldenNames -v ./...      # focused: the golden name table
go test -run TestGoldenMovieNames -v ./... # focused: the movie golden table
go test -run TestMovieFolderRename -v ./... # focused: movie folder renames
go test -run TestShowFolderRename -v ./...  # focused: TV season folder moves
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
2. **Planning is read-only.** `planDir`, `planPaths`, `planMovieFolders`,
   `planShowFolders`, `detect`, `detectMovie`, `detectShowSeason`,
   `renderTemplate`, and `renderMovie` must never touch the filesystem. Only
   `applyDir` writes — directly, or through `applyDirRenames` — and only after
   `runPlan` has confirmed `opt.apply` and zero conflicts.
3. **Conflicts abort the whole invocation.** If any `dirPlan.conflicts` or
   folder conflict is non-empty, nothing may move, and the exit code is `1`.
   Conflict kinds: duplicate target (case-insensitive, per directory), an
   existing target that is not itself a rename source, and empty or
   `/`/`:`-containing output names.
4. **All renames go through `applyDir`.** It stages every source to a temporary
   name, then moves each temporary to its target, so chains and case-only
   renames cannot collide. Movie folders and TV season folders are grouped per
   parent and applied by `applyDirRenames`, which calls `applyDir` with
   `entriesAreDirs` set. Do not call `os.Rename(source, target)` directly from a
   new code path.
5. **The file-name language is fixed and lives in `plan.go`.** `--movies` is a
   mode, not an extra pass: TV mode renames episodes with the two marker
   regexes (`SxxEyy`/`SxxEyyEzz`, then `1x02`) plus `normalizeShow`; movie mode
   renames movies with `detectMovie` and still runs `detect` as a skip guard, so
   an episode is never renamed as a movie. In movie mode a directory named
   directly on the command line is renamed with the movie template when
   `detectMovie` accepts its base name; nested directories are never renamed,
   and files inside run first. In TV mode a directory named directly on the
   command line is renamed to `<title> - S<NN>` when `detectShowSeason` finds a
   season marker (`S04`, `Season 4`, or an episode marker, which counts as its
   season); inside its show folder, or with no title text, the target is
   `Season NN` in place. A multi-season range or a folder with no marker is left
   alone. The movie vocabulary is the
   strong and weak token lists in `plan.go`. `--template` accepts `{title}`,
   `{season}`, `{episode}` in TV mode and `{title}`, `{year}` in movie mode;
   `--title` forces the title in both modes, and `--season` is rejected with
   `--movies`. Videos are classified
   solely by `options.exts`, and sidecars solely by video stem (never by
   extension). Never special-case an individual filename — extend the tables in
   `plan_test.go` (`TestGoldenNames`, `TestGoldenMovieNames`) instead, and do
   not relax existing expected names.

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
