# epfix

Fix episode and movie names in place. No API, no network.

Renames release names to library names. Prints a plan by default; `--apply`
renames.

## Install

```sh
brew tap ernilambar/tap && brew trust ernilambar/tap && brew install epfix
```

macOS 15+ on Apple silicon only. Prebuilt binary and source build:
[docs/install.md](docs/install.md).

## Usage

```sh
epfix ~/Downloads/Show.S04                      # preview
epfix --apply ~/Downloads/Show.S04              # rename
epfix --apply --title "Jack Ryan" ~/Downloads/Show.S04
                                                # force the series name
epfix --apply ~/Downloads/Show.S04E01.1080p.mkv # one file and its sidecars
epfix --movies --apply ~/Downloads/Movies       # rename movies
epfix --movies --apply ~/"Movies/Inception (2010) [720p] [BluRay] [YTS.MX]"
                                                # rename the folder and its file
epfix --apply ~/Downloads/Show.Name.S04.1080p.WEB-DL
                                                # rename it to Show Name - S04
```

- Episodes (`SxxEyy`, `1x02`) become `Show Name - S04E01`.
- Multi-episode files keep every episode: `S01E01E02`. A dash is a range
  (`S01E01-E03`, `S01E01-03`, `1x01-02`, `1x01-1x02`); a dot or a repeated `E`
  is a list (`S01E01.E02`, `S01E01E02`, `1x01.1x02`). Descending ranges
  (`S01E05-E03`) and `1x` runs that change season are left alone.
- With `--movies`, movies become `Movie (2026)`, or `Movie` without a year.
  Episodes are left alone.
- A folder passed as a path is renamed too: a TV season folder to
  `Show Name - S01` (`Season 01` inside its show folder), a movie folder with
  `--movies` to `Movie (2026)`. Multi-season packs (`S01-S05`) and folders
  without a season marker are left alone.
- Only files directly inside each path are renamed; subfolders are not scanned.
  Pass each season folder, e.g. `Show/*/`.
- Sidecars such as subtitles are renamed with their video.
- Any conflict aborts the whole run before a file moves. There is no undo:
  review the plan before `--apply`.

See `epfix --help` for all flags. Exit codes: `0` ok, `1` conflict or rename
failure, `2` usage error.

## License

[MIT](LICENSE) © [Nilambar Sharma](https://www.nilambar.net)
