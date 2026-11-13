# epfix

Fix episode names in place. No API, no guessing.

Renames TV episode files from release names to library names. Plans by default;
`--apply` to rename.

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
```

Files with `SxxEyy`, `SxxEyyEzz` or `1x02` in the name are renamed to
`{show} - S{season}E{episode}.mkv`; everything else is ignored. `--show`,
`--season` and `--template` override the derived name. Sidecars
(`<video stem>.<anything>`) are renamed with their video.

See `epfix --help` for all flags. Exit codes: `0` ok, `1` conflict or rename
failure, `2` usage error.

Conflicts (duplicate target, existing target, invalid output name) abort the
whole run before any file moves. There is no undo — review the preview before
`--apply`.

## License

[MIT](LICENSE) © [Nilambar Sharma](https://www.nilambar.net)
