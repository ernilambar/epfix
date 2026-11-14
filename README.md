# epfix

Fix episode and movie names in place. No API, no network.

Renames TV episode and movie files from release names to library names. Plans by
default; `--apply` to rename. Movie detection is opt-in with `--movies`.

## Install

### Homebrew (recommended)

```sh
brew tap ernilambar/tap
brew trust ernilambar/tap
brew install epfix
```

Upgrade later with `brew upgrade epfix`.

### Prebuilt binary

Download `epfix-darwin-arm64` from the
[latest release](https://github.com/ernilambar/epfix/releases/latest):

```sh
curl -L -o epfix https://github.com/ernilambar/epfix/releases/latest/download/epfix-darwin-arm64
chmod +x epfix
sudo mv epfix /usr/local/bin/
```

### From source

Requires Go 1.22+.

```sh
go build -o epfix .
```

## Usage

```sh
epfix ~/Downloads/Show.S04                     # preview
epfix --apply ~/Downloads/Show.S04             # rename
epfix -r --apply --show "Jack Ryan" ~/Media/TV # force the show name
epfix --apply ~/Downloads/Show.S04E01.1080p.mkv # one file and its sidecars
epfix --movies ~/Downloads/Movies              # preview movies
epfix --movies --apply ~/Downloads/Movies      # rename movies
```

Files with `SxxEyy`, `SxxEyyEzz` or `1x02` in the name are renamed to
`{show} - S{season}E{episode}.mkv`. `--show`, `--season` and `--template`
override the derived name. Sidecars (`<video stem>.<anything>`) are renamed
with their video. A path may be a directory or a video file: a file argument
renames only that file and the sidecars it owns, and `-r` applies to
directories only. Overlapping paths are planned once, whatever the spelling, so
`epfix . .` and `epfix -r DIR DIR` are harmless.

### Movies

`--movies` switches to movie mode: videos are renamed to `{title} ({year})`, or
`{title}` when the name has no year, and episodes are left alone. Run once per
mode when a directory holds both. `--template` sets the name in either mode; in
movie mode it accepts only `{title}` and `{year}`, and `--show`/`--season` do
not apply.

The name is cut at the year or at the first quality, source, codec, audio or
size tag (`1080p`, `WEB-DL`, `x265`, `DDP5.1`, `1.6GB`), after dropping a
leading release-site prefix (`www.site.com - `) or `[group]` tag. Names that
look like episodes or season packs are skipped, and so are names with no such
tag whose words are separated by spaces only.

Without a year there is nothing to hold back the language and edition words, so
a title that ends in one loses it: `Johnny.English.1080p` becomes `Johnny`.
Review the preview before `--apply`.

See `epfix --help` for all flags. Exit codes: `0` ok, `1` conflict or rename
failure, `2` usage error.

Conflicts (duplicate target, existing target, invalid output name) abort the
whole run before any file moves. There is no undo — review the preview before
`--apply`.

## License

[MIT](LICENSE) © [Nilambar Sharma](https://www.nilambar.net)
