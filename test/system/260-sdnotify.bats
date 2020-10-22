#!/usr/bin/env bats   -*- bats -*-
#
# Tests for systemd sdnotify
#

load helpers

# Shared throughout this module: PID of socat process, and path to its log
_SOCAT_PID=
_SOCAT_LOG=

function setup() {
    skip_if_remote "systemd tests are meaningless over remote"

    # Skip if systemd is not running
    systemctl list-units &>/dev/null || skip "systemd not available"

    # sdnotify fails with runc 1.0.0-3-dev2 on Ubuntu. Let's just
    # assume that we work only with crun, nothing else.
    run_podman info --format '{{ .Host.OCIRuntime.Name }}'
    if [[ "$output" != "crun" ]]; then
        skip "this test only works with crun, not '$output'"
    fi

    basic_setup
}

function teardown() {
    unset NOTIFY_SOCKET

    _stop_socat

    basic_teardown
}

###############################################################################
# BEGIN helpers

# Run socat process on a socket, logging to well-known path. Each received
# packet is logged with a newline appended, for ease of parsing the log file.
function _start_socat() {
    _SOCAT_LOG="$PODMAN_TMPDIR/socat.log"

    rm -f $_SOCAT_LOG
    socat unix-recvfrom:"$NOTIFY_SOCKET",fork \
          system:"(cat;echo) >> $_SOCAT_LOG" &
    _SOCAT_PID=$!
}

# Stop the socat background process and clean up logs
function _stop_socat() {
    if [[ -n "$_SOCAT_PID" ]]; then
        kill $_SOCAT_PID
    fi
    _SOCAT_PID=

    if [[ -n "$_SOCAT_LOG" ]]; then
        rm -f $_SOCAT_LOG
    fi
}

# Check that MAINPID=xxxxx points to a running conmon process
function _assert_mainpid_is_conmon() {
    local mainpid=$(expr "$1" : "MAINPID=\([0-9]\+\)")
    test -n "$mainpid" || die "Could not parse '$1' as 'MAINPID=nnnn'"

    test -d /proc/$mainpid || die "sdnotify MAINPID=$mainpid - but /proc/$mainpid does not exist"

    # e.g. /proc/12345/exe -> /usr/bin/conmon
    local mainpid_bin=$(readlink /proc/$mainpid/exe)
    is "$mainpid_bin" ".*/conmon" "sdnotify MAINPID=$mainpid is conmon process"
}

# END   helpers
###############################################################################
# BEGIN tests themselves

@test "sdnotify : ignore" {
    export NOTIFY_SOCKET=$PODMAN_TMPDIR/ignore.sock
    _start_socat

    run_podman 1 run --rm --sdnotify=ignore $IMAGE printenv NOTIFY_SOCKET
    is "$output" "" "\$NOTIFY_SOCKET in container"

    is "$(< $_SOCAT_LOG)" "" "nothing received on socket"
    _stop_socat
}

@test "sdnotify : conmon" {
    export NOTIFY_SOCKET=$PODMAN_TMPDIR/conmon.sock
    _start_socat

    run_podman run -d --name sdnotify_conmon_c \
               --sdnotify=conmon \
               $IMAGE \
               sh -c 'printenv NOTIFY_SOCKET;echo READY;while ! test -f /stop;do sleep 0.1;done'
    cid="$output"
    wait_for_ready $cid

    run_podman logs sdnotify_conmon_c
    is "$output" "READY" "\$NOTIFY_SOCKET in container"

    run cat $_SOCAT_LOG
    is "${lines[-1]}" "READY=1" "final output from sdnotify"

    _assert_mainpid_is_conmon "${lines[0]}"

    # Done. Stop container, clean up.
    run_podman exec $cid touch /stop
    run_podman wait $cid
    run_podman rm $cid
    _stop_socat
}

@test "sdnotify : container" {
    # Sigh... we need to pull a humongous image because it has systemd-notify.
    # (IMPORTANT: fedora:32 and above silently removed systemd-notify; this
    # caused CI to hang. That's why we explicitly require fedora:31)
    # FIXME: is there a smaller image we could use?
    local _FEDORA="$PODMAN_TEST_IMAGE_REGISTRY/$PODMAN_TEST_IMAGE_USER/fedora:31"
    # Pull that image. Retry in case of flakes.
    run_podman pull $_FEDORA || \
        run_podman pull $_FEDORA || \
        run_podman pull $_FEDORA

    export NOTIFY_SOCKET=$PODMAN_TMPDIR/container.sock
    _start_socat

    run_podman run -d --sdnotify=container $_FEDORA \
               sh -c 'printenv NOTIFY_SOCKET;echo READY;systemd-notify --ready;while ! test -f /stop;do sleep 0.1;done'
    cid="$output"
    wait_for_ready $cid

    run_podman logs $cid
    is "${lines[0]}" "/.*/container\.sock/notify" "NOTIFY_SOCKET is passed to container"

    # With container, READY=1 isn't necessarily the last message received;
    # just look for it anywhere in received messages
    run cat $_SOCAT_LOG
    is "$output" ".*READY=1" "received READY=1 through notify socket"

    _assert_mainpid_is_conmon "${lines[0]}"

    # Done. Stop container, clean up.
    run_podman exec $cid touch /stop
    run_podman wait $cid
    run_podman rm $cid
    run_podman rmi $_FEDORA
    _stop_socat
}

# vim: filetype=sh
