# getRelease

getRelease installs binaries from GitHub releases onto the local machine and keeps track of what it installed so it can upgrade them later.

## Language

### Releases

**Release**:
A tagged GitHub release of a repository.
_Avoid_: Version, build

**Asset**:
One downloadable file attached to a Release: either an archive or a bare binary.
_Avoid_: Download, payload, artifact

### Supply-chain safety

**Cooldown**:
The minimum age, in days, that a Release and the Asset chosen from it must reach before getRelease will install them.
_Avoid_: Delay, quarantine, waiting period

**Fallback release**:
The newest Release old enough to pass the Cooldown, used instead of the latest Release when the latest is too young. A Fallback release is never used when the user asked for a specific tag.
_Avoid_: Previous release, older release

**Trusted owner**:
A GitHub owner on a specific Host whose Releases skip the Cooldown.
_Avoid_: Allowlisted owner, exempt owner

### Installed software

**Package**:
A GitHub repository whose Release getRelease has installed and tracks for upgrades, identified by its Host plus owner/repo.
_Avoid_: Target, install, record, tracked install

**History**:
The persisted list of Packages getRelease has installed, with the Release, Host, Binaries, and Pin level of each. Upgrades, pinning, and shell completion read from it.
_Avoid_: Install log, registry, database

**Host**:
The GitHub site a Package comes from: `github.com` by default, or a GitHub Enterprise Cloud `*.ghe.com` tenant.
_Avoid_: Server, instance, domain

**Binary**:
An executable that getRelease installs from an Asset. It has an original name (as it appears in the Asset) and an Install-as name.
_Avoid_: Executable, program, tool

**Install-as name**:
The name a Binary has once installed on disk. It equals the original name unless the user renamed it.
_Avoid_: Alias, installed name, rename

**Install directory**:
The directory new Binaries are installed into. Changing it does not move Binaries already installed; History keeps each one at the path where it was installed until a Relocation.
_Avoid_: Install path (that is where one Binary lives), target directory, bin dir

**Relocation**:
Moving every Binary recorded in History to a new Install directory, updating its recorded path and removing the old copy. The new Install directory becomes the configured one.
_Avoid_: Migration, move, transfer

**Pin level**:
How far upgrades of a Package may move from its current Release: `patch` (no movement), `minor` (patch releases only), or `major` (minor and patch releases only).
_Avoid_: Pin policy, semver ceiling, version lock

**Target**:
What the user types to choose an existing Package on the command line: a binary name, owner/repo, or host/owner/repo. A bare owner/repo that matches Packages on more than one Host is ambiguous and is rejected.
_Avoid_: Package (when meaning the selector rather than the thing selected)
