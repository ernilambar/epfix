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
epfix ~/Downloads/Show.S04                      # preview
epfix --apply ~/Downloads/Show.S04              # rename
epfix -r --apply --title "Jack Ryan" ~/Media/TV # force the series name
epfix --apply ~/Downloads/Show.S04E01.1080p.mkv # one file and its sidecars
epfix --movies --apply ~/Downloads/Movies       # rename movies
```

- Episodes (`SxxEyy`, `1x02`) become `{title} - S{season}E{episode}`.
- With `--movies`, movies become `{title} ({year})`, or `{title}` without a
  year. Episodes are left alone in this mode.
- Sidecars such as subtitles are renamed with their video.
- Any conflict aborts the whole run before a file moves. There is no undo, so
  review the preview before `--apply`.

See `epfix --help` for all flags. Exit codes: `0` ok, `1` conflict or rename
failure, `2` usage error.

## License

[MIT](LICENSE) © [Nilambar Sharma](https://www.nilambar.net)
