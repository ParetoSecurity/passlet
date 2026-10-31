import Foundation

enum UpdateInstall {
    enum InvalidBundle: LocalizedError {
        case metadata
        var errorDescription: String? { "Update rejected: bundle identity or version does not match the requested release." }
    }

    static func validate(installed: URL, downloaded: URL, release: PassletRelease) throws {
        func metadata(_ app: URL) throws -> [String: Any] {
            let data = try Data(contentsOf: app.appendingPathComponent("Contents/Info.plist"))
            return try PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any] ?? [:]
        }
        let current = try metadata(installed)
        let next = try metadata(downloaded)
        let expected = String(release.tagName.dropFirst())
        guard let identifier = current["CFBundleIdentifier"] as? String, !identifier.isEmpty,
              next["CFBundleIdentifier"] as? String == identifier,
              next["CFBundleExecutable"] as? String == "PassletHelper",
              next["CFBundlePackageType"] as? String == "APPL",
              next["CFBundleShortVersionString"] as? String == expected,
              next["CFBundleVersion"] as? String == expected,
              release.tagName == "v\(expected)", !release.draft, !release.prerelease,
              let version = current["CFBundleShortVersionString"] as? String,
              !UpdateCheck.parseVersion(version).isEmpty,
              !release.version.isEmpty,
              release.version.allSatisfy({ $0 >= 0 }),
              release.version.map(String.init).joined(separator: ".") == expected,
              UpdateCheck.isNewer(release.version, than: UpdateCheck.parseVersion(version)) else {
            throw InvalidBundle.metadata
        }
        let menu = downloaded.appendingPathComponent("Contents/Helpers/PassletMenu.app")
        let menuInfo = try metadata(menu)
        guard menuInfo["CFBundleIdentifier"] as? String == identifier + ".menu",
              menuInfo["CFBundleExecutable"] as? String == "PassletMenu",
              menuInfo["CFBundleShortVersionString"] as? String == expected,
              menuInfo["CFBundleVersion"] as? String == expected else { throw InvalidBundle.metadata }
        for executable in [downloaded.appendingPathComponent("Contents/MacOS/PassletHelper"),
                           downloaded.appendingPathComponent("Contents/MacOS/passlet"),
                           menu.appendingPathComponent("Contents/MacOS/PassletMenu")] {
            guard FileManager.default.isExecutableFile(atPath: executable.path) else { throw InvalidBundle.metadata }
        }
    }

    static func replacementScript(installed: URL, downloaded: URL) -> String {
        // Never delete an earlier recovery copy or overwrite another update's staging directory.
        """
        set -eu
        target=\(quoted(installed.path))
        source=\(quoted(downloaded.path))
        backup="$target.previous"
        lock="$target.update-lock"
        /bin/mkdir "$lock" || { echo 'Another update or interrupted update needs attention.' >&2; exit 1; }
        staged="$lock/Passlet.app"
        moved=0
        committed=0
        cleanup() {
            status=$?
            trap - EXIT HUP INT TERM
            if [ "$moved" = 1 ] && [ "$committed" = 0 ]; then
                if ! /bin/mv "$backup" "$target"; then
                    echo "Restore failed. The previous app is preserved at $backup." >&2
                    exit 1
                fi
            fi
            /bin/rm -rf "$lock"
            exit "$status"
        }
        trap cleanup EXIT
        trap 'exit 1' HUP INT TERM
        if [ -e "$backup" ] || [ -L "$backup" ]; then
            echo "An earlier app is preserved at $backup. Recover it before updating." >&2
            exit 1
        fi
        /usr/bin/ditto "$source" "$staged"
        /bin/mv "$target" "$backup"
        moved=1
        /bin/mv "$staged" "$target"
        committed=1
        /bin/rm -rf "$backup"
        """
    }

    private static func quoted(_ path: String) -> String {
        "'" + path.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}
