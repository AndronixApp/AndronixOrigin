#!/bin/sh
# Andronix bwrap stand-in. bubblewrap needs user namespaces, which Android
# and proot don't give apps, so there are no sandboxes inside the distro.
#
# glycin (GdkPixbuf's image loading) probes bwrap with
# `bwrap ... --unshare-all ... /usr/bin/true`; we answer as bwrap does on a
# system without user namespaces, so glycin runs its loaders unsandboxed.
# That also skips the RLIMIT_AS cap it puts on sandboxed loaders, which is
# far too small on a phone (every image load fails: black desktop). Other
# callers get their command run directly. This is POSIX sh on purpose: it
# must start even under such a memory cap. Log: /tmp/andronix-bwrap.log.
log=/tmp/andronix-bwrap.log
note() { [ -f "$log" ] && [ "$(wc -c <"$log")" -gt 65536 ] && rm -f "$log"; echo "$1 uid=$(id -u) $2" >>"$log" 2>/dev/null; }

unshare_all=0 chdir=""
for a in "$@"; do last=$a; done
for a in "$@"; do [ "$a" = --unshare-all ] && unshare_all=1; done
if [ "$unshare_all" = 1 ] && [ "${last:-}" = /usr/bin/true ]; then
    note probe /usr/bin/true
    echo "bwrap: Creating new namespace failed: Operation not permitted (andronix: no user namespaces under proot)" >&2
    exit 1
fi

while [ $# -gt 0 ]; do
    case "$1" in
        --) shift; break ;;
        --clearenv)
            for v in $(env | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p'); do
                case "$v" in PWD|OLDPWD|SHLVL) ;; *) unset "$v" 2>/dev/null ;; esac
            done
            shift ;;
        --setenv) export "$2=$3"; shift 3 ;;
        --unsetenv) unset "$2"; shift 2 ;;
        --chdir) chdir=$2; shift 2 ;;
        --overlay) shift 4 ;;
        --bind|--bind-try|--bind-data|--chmod|--dev-bind|--dev-bind-try|--file|--ro-bind|--ro-bind-data|--ro-bind-try|--symlink)
            shift 3 ;;
        --args|--argv0|--block-fd|--cap-add|--cap-drop|--dev|--dir|--exec-label|--file-label|--gid|--hostname|--info-fd|\
        --json-status-fd|--lock-file|--mqueue|--overlay-src|--perms|--pidns|--proc|--remount-ro|--ro-overlay|--seccomp|\
        --add-seccomp-fd|--size|--sync-fd|--tmp-overlay|--tmpfs|--uid|--userns|--userns2|--userns-block-fd)
            shift 2 ;;
        --*) shift ;;
        *) break ;;
    esac
done
[ $# -gt 0 ] || { echo "bwrap (andronix): no command given" >&2; exit 1; }
note run "$*"
[ -n "$chdir" ] && { cd "$chdir" || exit 1; }
exec "$@"
