# Dev packs

Optional developer toolsets that sit on top of any Andronix distro, free or Modded:

```
andronix pack list   [<distro>]
andronix pack add    [<distro>] <pack>...     python | node | java | go | db
andronix pack remove [<distro>] <pack>...
```

- **From Termux,** pass the distro, e.g. `andronix pack add debian python`. The binary runs the step as root inside the distro, the same way the installer runs its package steps.
- **Inside the distro,** leave the distro out. Use `sudo andronix pack add python`, since `/usr/local/bin/andronix` is the same binary. On Manjaro, which has no sudo under proot, run it from Termux.

## Packs are data

One file per pack: `mods/packs/<id>.conf`. They're plain `KEY=value` files like `desktops/*.conf`, embedded in the binary.

| Key | Meaning |
|---|---|
| `PACK_ID`, `PACK_NAME`, `PACK_DESC` | id, display name, one line for `pack list` |
| `PACK_PKGS_<family>` | packages for apt, dnf, pacman, apk and xbps. `a\|b` means the first one the distro has; `x?` means optional, skipped if the distro lacks it. Versions are the distro's own, with no third-party repos. |
| `PACK_FILES` | `src:dest:mode` entries copied from `mods/packs/files/` (the `andronix-postgres` helper) |
| `PACK_POST` | shell run as root after the install (e.g. `corepack enable`) |
| `PACK_CHECK` | shell run as root afterwards; if it fails, the pack failed |
| `PACK_HINT` | one line shown when the pack is ready |
| `PACK_DISK_MB` | an estimate for the free-space check |

The installed state lives in `/etc/andronix/packs/<id>`, which records the exact packages that were installed. `remove` takes out exactly those with the family's remove-and-autoremove command, plus `PACK_FILES`.

## The packs

| Pack | apt (Debian 13 / Ubuntu 26.04 / Kali) | pacman (Manjaro ARM) | dnf (Fedora 44) | Notes |
|---|---|---|---|---|
| python | python3 (3.13 / 3.14 / 3.14), pip, venv, dev, pipx, build-essential | python 3.11, pip, pipx, base-devel | python3 3.14, pip, devel, pipx, gcc | `python3 -m venv` works; pipx for tools |
| node | nodejs (20 / 22 / 24), npm, node-corepack | nodejs 21, npm, corepack if present | the versioned nodejs stream | `corepack enable` for yarn and pnpm |
| java | default-jdk (21 / 25 / 25), maven | jdk21-openjdk, else jdk-openjdk (21 on ARM), maven | java-25-openjdk-devel, maven | the JDK is always an LTS |
| go | golang-go (1.24 / 1.26 / 1.26), gopls if present | go 1.22, gopls | golang 1.26, gopls | |
| db | postgresql (17 / 18 / 18), client, sqlite3, sqlitebrowser | postgresql 16, sqlite, sqlitebrowser | postgresql-server 18, sqlite, sqlitebrowser | `sudo andronix-postgres start` |

- **Why a PostgreSQL helper:** proot has no systemd, so `andronix-postgres` runs the server instead.
- **On the first start it:**
  - creates a cluster in `/var/lib/andronix-postgres`;
  - listens only on the socket and 127.0.0.1, with peer auth locally and scram-sha-256 over TCP;
  - uses mmap shared memory, which works under proot;
  - creates a superuser role and a database named after the user, so plain `psql` works.
- **Other commands:** `stop`, `status`, and `check` (start, query, stop), which the pack test uses.
- **GUI client:** DB Browser for SQLite, which every family packages. pgAdmin isn't packaged anywhere; DBeaver is only on Kali.

## Implementation

- **The Go command** is `internal/app/pack.go`. `mods/packs/pack.sh` is the reference implementation it follows: POSIX sh, run as root inside the distro.
- **Steps:** check free space against the `PACK_DISK_MB` total, refresh, resolve alternatives and optionals, install in one transaction, copy `PACK_FILES`, run `PACK_POST`, record, then run `PACK_CHECK`.
- **Where it runs:**
  - From Termux, the steps run as root through proot. With no distro named and only one installed, that one is used.
  - Inside a distro (`/etc/andronix-release` exists), they run directly and need root.
- **Checking a package is available:**
  - apt uses `apt-cache policy` and needs a real `Candidate`; `apt-cache show` also succeeds for packages with nothing installable.
  - pacman also accepts groups.
- **The record** has two lists:
  - `PACKAGES`: the packages the pack newly installed.
  - `NEEDS`: everything it uses.
- **Remove** takes out the pack's `PACKAGES`, except any that another added pack installed or needs. A kept package passes to a pack that needs it, so removing that pack later takes it out.
- **`PACK_PRE_REMOVE`** is shell run before remove. The db pack uses it to stop the server.
- **`PACK_PURGE`** lists data the pack's programs made; for db, that's `/var/lib/andronix-postgres` and the log.
  - Remove keeps the data and says where it is. `pack remove --purge <pack>` deletes it.
- **A failed check** leaves the record with `CHECKED=no`. `pack list` shows the pack as failed, `add` retries it, and `remove` works. Checks time out after 10 minutes.
- **PostgreSQL and System V shared memory:** PostgreSQL always makes a small System V segment. Under proot, Termux's `--sysvipc` helper deadlocks with initdb (Android 17 emulator).
  - `andronix-postgres` preloads `/usr/local/lib/andronix/libandronix-shm.so` (`guest/preload/shm.c`) for the server processes. It keeps segments as files in `/tmp/.andronix-shm`, and a flock stands in for the attach count.
  - initdb runs in `<data>.new` and is moved in place only when complete. An unfinished cluster from before this fix is moved aside to `<data>.broken-<time>`, never deleted.
- **Tests:**
  - `tests/smoke.sh` uses small test packs through `ANDRONIX_DATA`.
  - `tests/packs.sh <distro> [pack...]` runs the real packs through the Go command.
- `mods/packs/test-packs.sh <distro> <de>` installs an edition in the Termux-like box and runs every pack (add and check) plus one remove.
- The results are below.

## Results (aarch64, Docker box, proot as on a phone)

| Edition | python | node | java | go | db | remove |
|---|---|---|---|---|---|---|
