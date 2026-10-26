# Reproducible builds with Guix

These scripts build p2pool-go-VTC for the platforms the CI build does, Linux
and Windows (x86-64), the same way on every machine: anyone who builds the same
commit gets byte-for-byte the same zips. Then they sign the list of the zips'
hashes, so people can check that a release really came from this source.

- `guix-build` builds the zips.
- `guix-sign` checks them, lists their hashes in `SHA256SUMS` and signs that
  file.

The zips hold what CI's do (the binary, `web/static`, `LICENSE` and
`README.md`), are named the same way, and the binary is stamped with the same
version string.

## Before you start (once per machine)

1. **Linux, or WSL 2 on Windows**, with
   [Guix](https://guix.gnu.org/manual/en/html_node/Binary-Installation.html)
   installed and `guix-daemon` running.
2. **A clone of this repository** in Linux (in WSL, in your Linux home folder,
   not under `/mnt/c`).
3. **Your GPG key**, to sign releases. On Windows, keep it in
   [GnuPG](https://gnupg.org/download/) or [Gpg4win](https://gpg4win.org/);
   `guix-sign` uses that from WSL. Tell git which key it is, in the Linux
   clone:

   ```
   git config user.signingkey <your key ID>
   ```

The first build downloads Go and a few tools from the pinned Guix commit. A
machine that has built Bitcoin Core v32, Vertcoin Core's 32.x branch or the
One Click Miner already has that Guix checkout and most of the packages, and
takes a few minutes.

## Making a release, step by step

The example is version `v1.0.0`; use your own.

**1. Tag the commit** (wherever you commit):

```
git tag -s v1.0.0 -m v1.0.0
git push origin v1.0.0
```

**2. Check out the tag in the Linux clone:**

```
git fetch origin --tags --force
git checkout v1.0.0
```

**3. Build:**

```
./contrib/guix/guix-build
```

It names everything after the tag on the commit, so you don't need to give the
version. The zips are in `guix-build-v1.0.0/output/<host>/`, next to the bare
binary and a `SHA256SUMS.part`. Compare those hashes with someone else's build
of the same commit.

**4. Sign, and copy the release files to Windows:**

```
RELEASE_DIR=/mnt/c/Users/<you>/Downloads/v1.0.0 ./contrib/guix/guix-sign
```

It checks the outputs, writes `SHA256SUMS`, and asks for your GPG passphrase
to sign it. Then it copies four files to `Downloads\v1.0.0`:

| file | what it is |
|---|---|
| `p2pool-go-vtc-v1.0.0-linux-x64.zip` | the Linux release |
| `p2pool-go-vtc-v1.0.0-windows-x64.zip` | the Windows release |
| `SHA256SUMS` | the SHA-256 of each zip and of the binary inside it |
| `SHA256SUMS.asc` | your signature of `SHA256SUMS` |

**5. Upload** those files to the GitHub release, in place of the
`SHA256SUMS` the CI's draft release made.

Anyone can check a release with `gpg --verify SHA256SUMS.asc` and
`sha256sum --check --ignore-missing SHA256SUMS`.

## Options

`guix-build`:

| variable | meaning |
|---|---|
| `HOSTS` | `x86_64-linux-gnu`, `x86_64-w64-mingw32`, or both (the default) |
| `VERSION` | e.g. `v1.0.0`: the zips are named after it and the binary is stamped `v1.0.0-g<commit>`. Unset, it is the tag on HEAD. With no tag (or `VERSION=`), the zips are named `dev-<commit>` and the binary `dev-g<commit>`, as CI does for a branch |
| `JOBS` | parallel jobs, default `nproc` |
| `CACHE_PATH` | where Go modules are kept, default `./guix-cache` |
| `SUBSTITUTE_URLS` | substitute servers to use instead of the daemon's defaults |
| `ADDITIONAL_GUIX_COMMON_FLAGS` | extra flags for `guix time-machine` |

`guix-sign`:

| variable | meaning |
|---|---|
| `VERSION` | the version it was built as, by default the tag on HEAD |
| `GPG` | gpg to sign with |
| `SIGNER` | key ID to sign with, by default git's `user.signingkey`, else gpg's default key |
| `RELEASE_DIR` | where to copy the release assets |

## What it does

1. `git archive HEAD` makes the source tarball, so only committed files are
   built. The script refuses to run with uncommitted changes.
2. **Fetch** (`libexec/fetch.sh`, the only step with network): `go mod
   download` into `CACHE_PATH`. Go checks each module against `go.sum`.
3. **Build** (`libexec/build.sh`, one container per host, no network): `go
   build -trimpath -buildvcs=false` with `CGO_ENABLED=0`, as CI builds it, so
   the binaries are static and Go's own linker makes both. The files in the
   zip get the commit's time and are added in sorted order.

Go comes from the Guix commit Bitcoin Core v32.0 pins (`c5eee333`), whose
`go@1.25` is 1.25.8. Guix patches its Go to read `/etc/services`,
`/etc/protocols` and the time zone database from `/gnu/store`, which a
normal Linux system doesn't have, so `build.sh` undoes those edits on a copy
of GOROOT before building, and fails if a binary still names `/gnu/store`.
With that, both binaries are byte-for-byte what upstream Go 1.25.8 builds
from the same commit with the same flags (`CGO_ENABLED=0 go build -trimpath
-buildvcs=false -ldflags "-s -w -buildid= -X ..."`), so they behave like any
other Go build of the node. That match is not a way to verify a release:
upstream Go is a prebuilt binary, while Guix builds its Go from source at a
pinned commit. A release is verified by others running `guix-build` on the
same commit and comparing `SHA256SUMS`. The node needs no C toolchain, so unlike Bitcoin Core and
the One Click Miner there are no cross compilers or patches here.

`guix-build` doesn't run the tests or `go vet`; the CI build does that for
every commit.
