# Install

Requires macOS 15 or newer on Apple silicon; Intel Macs are not supported.

## Homebrew (recommended)

```sh
brew tap ernilambar/tap
brew trust ernilambar/tap
brew install epfix
```

Upgrade later with `brew upgrade epfix`.

## Prebuilt binary

Download `epfix-darwin-arm64` from the
[latest release](https://github.com/ernilambar/epfix/releases/latest):

```sh
curl -L -o epfix https://github.com/ernilambar/epfix/releases/latest/download/epfix-darwin-arm64
chmod +x epfix
sudo mv epfix /usr/local/bin/
```

## From source

Requires Go 1.27+.

```sh
GOARCH=arm64 go build -ldflags '-macos 15.0' -o epfix .
```
