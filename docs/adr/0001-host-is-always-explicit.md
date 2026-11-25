# Host is always explicit; ambient host settings are ignored

getRelease targets `github.com` unless the Host is named in the command itself (a `host/owner/repo` argument, a URL, or `--host`) or comes from the Package's install history. `GH_HOST` is deliberately ignored, and `--host` has no environment variable or config key. A leftover shell or CI setting must never silently send an install or upgrade to a different Host. If it did, the user could end up running Binaries from a tenant they didn't intend. This differs from the `gh` CLI, so don't "fix" it by honouring `GH_HOST`.
