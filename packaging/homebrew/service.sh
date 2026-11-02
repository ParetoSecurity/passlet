#!/bin/sh
# Called only by the Homebrew cask, as the installing user.
set -eu
[ "$(id -u)" != 0 ] || { echo 'Install Passlet as your logged-in user, not root.' >&2; exit 1; }
action=$1
app=$2
prefix=$3
label=dev.passlet.agent
file="$HOME/Library/LaunchAgents/$label.plist"
domain="gui/$(id -u)"

# Never replace another manager's service, including Home Manager store links.
if [ -e "$file" ] || [ -L "$file" ]; then
    if [ -L "$file" ] || [ "$(/usr/bin/plutil -extract PassletPackageManager raw -o - "$file" 2>/dev/null || true)" != homebrew ]; then
        echo 'Passlet service belongs to another installer. Disable it there before using Homebrew.' >&2
        exit 1
    fi
fi
case "$action" in
    install)
        /bin/launchctl print "$domain" >/dev/null 2>&1 || {
            echo 'Install Passlet from your logged-in Mac desktop session.' >&2; exit 1;
        }
        [ -x "$app/Contents/MacOS/passlet" ]
        [ -x "$app/Contents/MacOS/PassletHelper" ]
        umask 077
        mkdir -p "$HOME/Library/LaunchAgents"
        temporary=$(mktemp "$HOME/Library/LaunchAgents/.passlet.XXXXXX")
        trap 'rm -f "$temporary"' EXIT HUP INT TERM
        /usr/bin/plutil -create xml1 "$temporary"
        /usr/bin/plutil -insert Label -string "$label" "$temporary"
        /usr/bin/plutil -insert AssociatedBundleIdentifiers -json '["dev.passlet.app"]' "$temporary"
        /usr/bin/plutil -insert PassletPackageManager -string homebrew "$temporary"
        /usr/bin/plutil -insert ProgramArguments -json '[]' "$temporary"
        /usr/bin/plutil -insert ProgramArguments.0 -string "$app/Contents/MacOS/passlet" "$temporary"
        /usr/bin/plutil -insert ProgramArguments.1 -string serve "$temporary"
        /usr/bin/plutil -insert ProgramArguments.2 -string --helper "$temporary"
        /usr/bin/plutil -insert ProgramArguments.3 -string "$app/Contents/MacOS/PassletHelper" "$temporary"
        /usr/bin/plutil -insert EnvironmentVariables -json '{}' "$temporary"
        /usr/bin/plutil -insert EnvironmentVariables.PATH -string "$prefix/bin:/usr/bin:/bin" "$temporary"
        /usr/bin/plutil -insert RunAtLoad -bool YES "$temporary"
        /usr/bin/plutil -insert KeepAlive -json '{}' "$temporary"
        /usr/bin/plutil -insert KeepAlive.SuccessfulExit -bool NO "$temporary"
        /usr/bin/plutil -insert ThrottleInterval -integer 10 "$temporary"
        /usr/bin/plutil -insert LimitLoadToSessionType -string Aqua "$temporary"
        /usr/bin/plutil -insert Umask -integer 63 "$temporary"
        if /bin/launchctl print "$domain/$label" >/dev/null 2>&1; then
            /bin/launchctl bootout --wait "$domain/$label"
        fi
        mv -f "$temporary" "$file"
        /bin/launchctl bootstrap "$domain" "$file"
        ;;
    uninstall)
        if [ -f "$file" ]; then
            if /bin/launchctl print "$domain/$label" >/dev/null 2>&1; then
                /bin/launchctl bootout --wait "$domain/$label"
            fi
            rm "$file"
        fi
        ;;
    *) echo 'Invalid Homebrew lifecycle operation.' >&2; exit 1 ;;
esac
