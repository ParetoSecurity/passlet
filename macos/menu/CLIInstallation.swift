import AppKit

/// Installs only a symlink; the executable stays in the signed app bundle.
enum CLIInstallation {
    static let appPath = "/Applications/Passlet.app"
    static let destination = URL(fileURLWithPath: "/usr/local/bin/passlet")

    enum InstallError: LocalizedError {
        case moveApp, conflict, failed(String)
        var errorDescription: String? {
            switch self {
            case .moveApp: return "Move Passlet.app to /Applications before installing the command line tool."
            case .conflict: return "A file already exists at /usr/local/bin/passlet. It was not replaced. Manage it with its original installer or Home Manager."
            case .failed(let message): return message
            }
        }
    }

    static func isLinked(_ destination: URL, to source: URL) -> Bool {
        FileManager.default.isExecutableFile(atPath: destination.path)
            && destination.resolvingSymlinksInPath() == source.resolvingSymlinksInPath()
    }

    static func available(app: URL) -> Bool {
        let source = app.appendingPathComponent("Contents/MacOS/passlet")
        let home = FileManager.default.homeDirectoryForCurrentUser
        let directories = (ProcessInfo.processInfo.environment["PATH"] ?? "").split(separator: ":").map(String.init)
            + ["/usr/local/bin", "/opt/homebrew/bin", home.appendingPathComponent(".local/bin").path]
        return directories.contains { isLinked(URL(fileURLWithPath: $0).appendingPathComponent("passlet"), to: source) }
    }

    // No force flag: existing files and symlinks, including broken links, survive.
    static func linkScript(source: URL, destination: URL) -> String {
        func quote(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'" }
        let target = quote(destination.path)
        return "set -eu; if [ -e \(target) ] || [ -L \(target) ]; then exit 1; fi; /bin/mkdir -p \(quote(destination.deletingLastPathComponent().path)); /bin/ln -sh \(quote(source.path)) \(target)"
    }

    static func install(app: URL) throws {
        guard app.standardizedFileURL.path == appPath else { throw InstallError.moveApp }
        let source = app.appendingPathComponent("Contents/MacOS/passlet")
        guard FileManager.default.isExecutableFile(atPath: source.path) else { throw InstallError.moveApp }
        if isLinked(destination, to: source) { return }
        if (try? FileManager.default.attributesOfItem(atPath: destination.path)) != nil {
            throw InstallError.conflict
        }
        let parent = destination.deletingLastPathComponent()
        if FileManager.default.isWritableFile(atPath: parent.path) {
            try FileManager.default.createSymbolicLink(at: destination, withDestinationURL: source)
        } else {
            let command = linkScript(source: source, destination: destination)
            let escaped = command.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
            var error: NSDictionary?
            guard let script = NSAppleScript(source: "do shell script \"\(escaped)\" with administrator privileges") else {
                throw InstallError.failed("Cannot request administrator approval.")
            }
            script.executeAndReturnError(&error)
            if let error {
                throw InstallError.failed(error[NSAppleScript.errorNumber] as? Int == -128
                    ? "CLI installation cancelled."
                    : "CLI installation failed. Check /usr/local/bin/passlet for a conflicting file.")
            }
        }
        guard isLinked(destination, to: source) else { throw InstallError.failed("The CLI link could not be verified.") }
    }
}
