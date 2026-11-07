#!/bin/sh
set -eu

for action in staple validate; do
    attempt=1
    while :; do
        if output=$(xcrun stapler "$action" "$1" 2>&1); then
            printf '%s\n' "$output"
            break
        else
            status=$?
        fi
        printf '%s\n' "$output" >&2
        # Error 68 also covers invalid tickets; retry only the observed connection loss.
        case "$status:$output" in
            68:*"NSURLErrorDomain Code=-1005"*) ;;
            *) exit "$status" ;;
        esac
        if [ "$attempt" -eq 4 ]; then
            exit "$status"
        fi
        delay=$((attempt * 15))
        printf 'Stapler %s lost its network connection; retrying in %ss (attempt %s/4).\n' "$action" "$delay" "$((attempt + 1))" >&2
        sleep "$delay"
        attempt=$((attempt + 1))
    done
done
