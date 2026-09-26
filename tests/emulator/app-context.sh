#!/usr/bin/env bash
# Run shell scripts INSIDE the Termux app process on an emulator, with the
# app's seccomp filter and SELinux domain (untrusted_app), the way users
# run andronix. `adb shell run-as com.termux` has neither: Go's faccessat2
# and pidfd syscalls pass there and crash (SIGSYS) in the real app.
#
#   tests/emulator/app-context.sh [-s SERIAL] <<'EOF'
#   andronix version
#   EOF
#
# Or source it (test-matrix.sh --app-context does) and use tsh_app, which
# takes the script on stdin like tsh: output on stdout, the script's exit
# code as its own.
#
# How: a small agent (~/.ac/agent) runs as a background job of the Termux
# session's shell, so it and everything it starts inherit the app's
# context. It is started once per boot by typing one command into
# TermuxActivity (adb shell input text). Jobs are dropped into ~/.ac/q with
# run-as; the agent runs each one and leaves its output and exit code in
# ~/.ac/out. ~/.ac/agent.ctx records the agent's Seccomp mode and SELinux
# context; tsh_app refuses to run if the agent isn't under the app filter.

AC_ADB=${ADB:-adb}
AC_PKG=${TERMUX_PKG:-com.termux}
AC_HOME=${T_HOME:-/data/data/com.termux/files/home}
AC_PREFIX=${T_PREFIX:-/data/data/com.termux/files/usr}
AC_DIR=$AC_HOME/.ac

ac_log() {
    if declare -f log >/dev/null; then log "$*"; else printf '%s  %s\n' "$(date +%H:%M:%S)" "$*" >&2; fi
}

# Every adb call reads from /dev/null: adb shell would otherwise eat the
# caller's stdin (the job script).
ac_runas() { "$AC_ADB" shell "run-as $AC_PKG sh -c '$1'" </dev/null 2>/dev/null | tr -d '\r'; }

# Copy stdin to a file under ~/.ac as the app user (atomic: tmp then mv).
ac_put() { # ac_put REL_PATH MODE
    local tmp stage; tmp=$(mktemp); cat >"$tmp"
    stage=/data/local/tmp/ac-$$-$RANDOM
    "$AC_ADB" push "$tmp" "$stage" </dev/null >/dev/null 2>&1 && rm -f "$tmp"
    "$AC_ADB" shell "chmod 644 $stage && run-as $AC_PKG sh -c 'cat $stage > $AC_DIR/$1.part && chmod $2 $AC_DIR/$1.part && mv $AC_DIR/$1.part $AC_DIR/$1'; rm -f $stage" </dev/null >/dev/null 2>&1
}

ac_install_agent() {
    ac_runas "mkdir -p $AC_DIR/q $AC_DIR/run $AC_DIR/out"
    ac_put agent 700 <<'AGENT'
# Andronix test agent. Runs in the Termux app process (see app-context.sh).
set -m  # each job gets its own process group, so a timeout can stop all of it
D=$HOME/.ac
mkdir -p "$D/q" "$D/run" "$D/out"
echo $$ >"$D/agent.pid"
{
    grep -E '^(Seccomp|NoNewPrivs):' /proc/self/status
    echo "context=$(tr -d '\0' </proc/self/attr/current 2>/dev/null)"
    echo "uid=$(id -u) android=$(getprop ro.build.version.release) sdk=$(getprop ro.build.version.sdk)"
} >"$D/agent.ctx"
while :; do
    date +%s >"$D/alive"
    for j in "$D"/q/*.sh; do
        [ -e "$j" ] || continue
        id=${j##*/}; id=${id%.sh}
        mv "$j" "$D/run/$id.sh" || continue
        ( bash "$D/run/$id.sh" </dev/null >"$D/out/$id.out" 2>&1
          echo $? >"$D/out/$id.rc.part" && mv "$D/out/$id.rc.part" "$D/out/$id.rc" ) &
        echo $! >"$D/run/$id.pid"
    done
    for k in "$D"/run/*.kill; do
        [ -e "$k" ] || continue
        id=${k##*/}; id=${id%.kill}
        p=$(cat "$D/run/$id.pid" 2>/dev/null)
        [ -n "$p" ] && { kill -TERM -- "-$p" 2>/dev/null; sleep 2; kill -KILL -- "-$p" 2>/dev/null; }
        echo 143 >"$D/out/$id.rc"
        rm -f "$k"
    done
    sleep 1
done
AGENT
    ac_put start 700 <<'START'
# Typed into the Termux session: start the agent in the background, return.
[ -f "$HOME/.ac/agent.pid" ] && kill "$(cat "$HOME/.ac/agent.pid")" 2>/dev/null
nohup bash "$HOME/.ac/agent" >"$HOME/.ac/agent.log" 2>&1 </dev/null &
clear
START
}

# Seconds since the agent last checked in (a large number if never).
ac_age() {
    local a; a=$(ac_runas "echo \$(( \$(date +%s) - \$(cat $AC_DIR/alive 2>/dev/null || echo 0) ))")
    echo "${a:-999999}"
}

# Put TermuxActivity in front with its terminal ready for keys.
ac_front() {
    "$AC_ADB" shell input keyevent KEYCODE_WAKEUP </dev/null
    "$AC_ADB" shell wm dismiss-keyguard >/dev/null 2>&1
    "$AC_ADB" shell svc power stayon true >/dev/null 2>&1
    "$AC_ADB" shell am start -n "$AC_PKG/.app.TermuxActivity" >/dev/null 2>&1
    local i
    for i in $(seq 1 15); do
        "$AC_ADB" shell dumpsys window 2>/dev/null | grep -E 'mCurrentFocus|mFocusedWindow' |
            grep -q "$AC_PKG/$AC_PKG.app.TermuxActivity" && return 0
        # A system dialog (notification permission, "app isn't responding")
        # can sit on top: go back once and bring Termux forward again.
        "$AC_ADB" shell input keyevent KEYCODE_BACK </dev/null
        sleep 1
        "$AC_ADB" shell am start -n "$AC_PKG/.app.TermuxActivity" >/dev/null 2>&1
        sleep 1
    done
    return 1
}

# Start the agent if it isn't running, and check it runs in the app's context.
ac_ensure() {
    [ "$(ac_age)" -le 10 ] && return 0
    ac_install_agent
    local try
    for try in 1 2 3; do
        ac_front || { ac_log "app-context: TermuxActivity didn't come to the front (try $try)"; continue; }
        sleep 2
        "$AC_ADB" shell input text "bash%s$AC_DIR/start" </dev/null
        "$AC_ADB" shell input keyevent KEYCODE_ENTER </dev/null
        local i
        for i in $(seq 1 20); do
            [ "$(ac_age)" -le 5 ] && break
            sleep 1
        done
        [ "$(ac_age)" -le 5 ] && break
        ac_log "app-context: agent didn't start (try $try)"
    done
    [ "$(ac_age)" -le 5 ] || return 1
    local ctx; ctx=$(ac_runas "cat $AC_DIR/agent.ctx")
    ac_log "app-context agent: $(echo "$ctx" | tr '\n' ' ')"
    # Seccomp: 2 is filter mode, what every Android app runs under.
    if ! echo "$ctx" | grep -qE '^Seccomp:[[:space:]]*2'; then
        ac_log "app-context: the agent is NOT under the app's seccomp filter; refusing to pretend"
        return 1
    fi
    return 0
}

# tsh_app: like tsh, but inside the Termux app process.
tsh_app() {
    local id rc body
    body=$(cat)  # first: nothing below may read the caller's stdin
    ac_ensure </dev/null || { echo "app-context: no agent in the Termux app" >&2; return 125; }
    id="j$(date +%s)$$$RANDOM"
    printf 'cd "$HOME"\nexport PATH="$HOME/.local/bin:$PATH"\n%s\n' "$body" | ac_put "q/$id.sh.tmp" 600
    ac_runas "mv $AC_DIR/q/$id.sh.tmp $AC_DIR/q/$id.sh"
    # Stopped (a timeout, or the caller killing a background session): stop
    # the job too.
    trap 'ac_runas "touch $AC_DIR/run/'"$id"'.kill"; trap - TERM INT; return 143' TERM INT
    while [ -z "$(ac_runas "ls $AC_DIR/out/$id.rc 2>/dev/null")" ]; do
        sleep 2
        # The agent stopped checking in (seen once on Android 16 mid-run):
        # start it again and keep waiting. The job writes its own exit code,
        # so it finishes even without the agent; if Termux itself died, the
        # caller's timeout ends the wait.
        if [ "$(ac_age)" -gt 30 ]; then
            echo "app-context: agent stopped while job $id ran; restarting it" >&2
            ac_ensure </dev/null >/dev/null 2>&1
        fi
    done
    trap - TERM INT
    "$AC_ADB" shell "run-as $AC_PKG cat $AC_DIR/out/$id.out" </dev/null 2>/dev/null
    rc=$(ac_runas "cat $AC_DIR/out/$id.rc")
    ac_runas "rm -f $AC_DIR/out/$id.out $AC_DIR/out/$id.rc $AC_DIR/run/$id.sh $AC_DIR/run/$id.pid"
    return "${rc:-1}"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    set -uo pipefail
    while [ $# -gt 0 ]; do
        case $1 in
            -s) export ANDROID_SERIAL=$2; shift ;;
            -h|--help) sed -n '2,21p' "$0"; exit 0 ;;
            *) echo "unknown option: $1" >&2; exit 2 ;;
        esac
        shift
    done
    if ! command -v "$AC_ADB" >/dev/null; then
        for d in "${ANDROID_HOME:-}" "$HOME/Library/Android/sdk" /opt/homebrew/share/android-commandlinetools; do
            [ -n "$d" ] && [ -x "$d/platform-tools/adb" ] && { AC_ADB=$d/platform-tools/adb; break; }
        done
    fi
    tsh_app
fi
