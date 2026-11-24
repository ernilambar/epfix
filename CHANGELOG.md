# Changelog

## X.X.X - YYYY-MM-DD
- Added: short flags `-a`, `-m`, `-t`, `-s`, `-e`, and `-V`
- Added: multi-episode ranges such as `S01E01-E03` and `1x01-02`
- Fixed: `--apply` after `--` triggered a rename
- Fixed: two folders renamed to one target could partly apply
- Fixed: sidecars renamed twice or left behind
- Fixed: `--movies` renamed episode files and folders
- Fixed: `-r` on a symlinked folder planned nothing
- Removed: `--template`, `--sidecars`, `--no-sidecars`, `--ext`, and `--quiet`

## 0.1.2 - 2026-11-14
- Added: rename movie folders named as path arguments in `--movies` mode
- Added: rename TV season folders named as path arguments to `Show Name - S01`

## 0.1.1 - 2026-11-14
- Added: `--movies` mode
- Added: accept video files as path arguments
- Fixed: movie detection false positives

## 0.1.0 - 2026-11-13
- Initial release
