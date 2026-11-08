#!/bin/sh
# kill-kammon.sh — stop running KAMmon instances.
#
# It finds and stops:
#   1. the compiled server binary — its process name is "kammon", however
#      it was started (go run, go build output, go install)
#   2. the "go run ./cmd/kammon ..." wrapper, when started with `go run`
#
# It matches only those two things, never the bare word "kammon", so
# editors, greps and other programs that mention kammon are left alone.
# It first asks politely with TERM, waits up to 5 seconds, and only then
# forces with KILL. Test runs (`*.test`) are not touched.

set -u

wrapped='go run ./cmd/kammon( |$)'

running() {
    pgrep -x kammon
    pgrep -f "$wrapped"
}

pids=$(running)
if [ -z "$pids" ]; then
    echo "no running kammon instances"
    exit 0
fi
echo "stopping kammon: $(echo $pids)"

# A suspended process (state T, e.g. from Ctrl-Z) does not act on TERM
# until it is continued, so wake it up first.
pkill -CONT -x kammon 2>/dev/null
pkill -CONT -f "$wrapped" 2>/dev/null
pkill -x kammon 2>/dev/null
pkill -f "$wrapped" 2>/dev/null

for i in 1 2 3 4 5; do
    sleep 1
    pids=$(running)
    if [ -z "$pids" ]; then
        echo "kammon stopped"
        exit 0
    fi
done

pkill -9 -x kammon 2>/dev/null
pkill -9 -f "$wrapped" 2>/dev/null
echo "kammon force stopped (would not exit on TERM)"
