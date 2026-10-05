## v1.4.3 (2026-10-04)

### Fix

- order cooldown output and recognize secondary rate limits
- **root**: report binaries a reinstall stops tracking
- **archive**: ignore libraries and helper scripts when finding binaries
- **history**: match owner and repo case-insensitively
- **upgrade**: don't downgrade releases newer than latest
- **install**: replace binaries atomically in CopyInstaller
- **github**: replace total download timeout with a stall timeout
- **archive**: decompress single-file .gz, .bz2, and .xz assets
- **upgrade**: match binaries whose names change between releases
- **config**: persist only the changed key when saving config

## v1.4.2 (2026-09-07)

### Fix

- **deps**: bump go.yaml.in/yaml/v3 from 3.0.4 to 3.0.5
- **deps**: bump github.com/mattn/go-isatty from 0.0.22 to 0.0.24

## v1.4.1 (2026-07-05)

### Fix

- **github**: scope gh auth tokens by host

## v1.4.0 (2026-07-05)

### Feat

- add release cooldown to harden against supply-chain attacks

## v1.3.0 (2026-07-04)

### Feat

- add authenticated GitHub API support with token resolution chain
- **build**: enable arm64 support for gorelease

## v1.2.2 (2026-07-03)

### Fix

- **completion**: add missing shell completion for format, config keys, and history remove

## v1.2.1 (2026-06-21)

### Fix

- **deps**: bump github.com/mattn/go-isatty from 0.0.20 to 0.0.22

## v1.2.0 (2026-04-11)

### Feat

- **pin**: add version pinning with pin/unpin commands and semver ceiling support

## v1.1.0 (2026-03-22)

### Feat

- **history**: add sorting and shell completion to history list

## v1.0.0 (2026-03-18)

### Feat

- initial release of getRelease tool to manage & automate GitHub package installs and upgrades
