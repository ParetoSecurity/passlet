import AppKit
import CryptoKit
import Foundation

// Self-update from GitHub Releases, ported from wgsplit. The download must
// match the published size and SHA-256, pass a strict signature check, and
// have the same Developer ID and team as the installed app.
enum SelfUpdater {
    enum UpdateError: LocalizedError {
        case notBundled
        case badDownload(String)
        case signatureMismatch(installed: String, downloaded: String)
        case cancelled

        var errorDescription: String? {
            switch self {
            case .notBundled:
                return "Updates need the signed Passlet.app, not a development build."
            case .badDownload(let why):
                return "Update download failed: \(why)"
            case .signatureMismatch(let installed, let downloaded):
                return "Update rejected: \"\(downloaded)\" does not match the installed app (\"\(installed)\")."
            case .cancelled:
                return "Update cancelled."
            }
        }
    }

    /// The outer Passlet.app. The menu app is at Contents/Helpers/PassletMenu.app.
    static var installedApp: URL? {
        let app = Bundle.main.bundleURL.deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let helper = app.appendingPathComponent("Contents/MacOS/PassletHelper")
        guard app.pathExtension == "app", FileManager.default.isExecutableFile(atPath: helper.path) else { return nil }
        return app
    }

    static var installedVersion: String {
        guard let app = installedApp, let bundle = Bundle(url: app),
              let version = bundle.infoDictionary?["CFBundleShortVersionString"] as? String else { return "dev" }
        return version
    }

    static func check() async throws -> PassletRelease? {
        guard installedApp != nil else { return nil }
        var request = URLRequest(url: UpdateCheck.releasesURL)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.cachePolicy = .reloadIgnoringLocalCacheData
        let (data, response) = try await URLSession.shared.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else {
            throw UpdateError.badDownload("GitHub returned an unexpected status")
        }
        return UpdateCheck.newest(try UpdateCheck.decodeReleases(data), installed: installedVersion)
    }

    /// Downloads, verifies and installs `release`, then relaunches the menu.
    /// It returns only on failure. On success the menu app terminates.
    static func installAndRelaunch(_ release: PassletRelease) async throws {
        guard let installed = installedApp else { throw UpdateError.notBundled }
        guard let archive = release.appAsset, let sums = release.checksumAsset else {
            throw UpdateError.badDownload("release has no macOS archive or checksum file")
        }

        let (sumsURL, sumsResponse) = try await URLSession.shared.download(from: sums.browserDownloadUrl)
        guard (sumsResponse as? HTTPURLResponse)?.statusCode == 200,
              let listing = try? String(contentsOf: sumsURL, encoding: .utf8),
              let expected = UpdateCheck.checksum(for: archive.name, in: listing) else {
            throw UpdateError.badDownload("checksum file is missing the macOS archive")
        }

        let (zipURL, zipResponse) = try await URLSession.shared.download(from: archive.browserDownloadUrl)
        guard (zipResponse as? HTTPURLResponse)?.statusCode == 200 else {
            throw UpdateError.badDownload("unexpected HTTP status")
        }
        let zip = try Data(contentsOf: zipURL)
        guard zip.count == archive.size else {
            throw UpdateError.badDownload("size mismatch (\(zip.count) of \(archive.size) bytes)")
        }
        let actual = SHA256.hash(data: zip).map { String(format: "%02x", $0) }.joined()
        guard actual == expected else { throw UpdateError.badDownload("SHA-256 mismatch") }

        let staging = FileManager.default.temporaryDirectory
            .appendingPathComponent("passlet-update-\(release.tagName)-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: staging) }
        // ditto keeps the code signature and stapled ticket intact.
        let unzip = run("/usr/bin/ditto", ["-xk", zipURL.path, staging.path])
        guard unzip.ok else { throw UpdateError.badDownload(unzip.stderr) }
        let downloaded = staging.appendingPathComponent("Passlet.app")
        guard FileManager.default.fileExists(atPath: downloaded.path) else {
            throw UpdateError.badDownload("archive contains no Passlet.app")
        }

        let strict = run("/usr/bin/codesign", ["--verify", "--deep", "--strict", downloaded.path])
        guard strict.ok else { throw UpdateError.badDownload("signature check failed") }
        let gatekeeper = run("/usr/sbin/spctl", ["--assess", "--type", "execute", downloaded.path])
        guard gatekeeper.ok else { throw UpdateError.badDownload("Gatekeeper rejected the download") }
        let installedSigner = signer(of: installed)
        let downloadedSigner = signer(of: downloaded)
        guard installedSigner.authority.hasPrefix("Developer ID Application:"),
              installedSigner == downloadedSigner else {
            throw UpdateError.signatureMismatch(installed: installedSigner.description,
                                                downloaded: downloadedSigner.description)
        }

        try UpdateInstall.validate(installed: installed, downloaded: downloaded, release: release)
        try replace(installed, with: downloaded)
        restartService()
        relaunch(installed.appendingPathComponent("Contents/Helpers/PassletMenu.app"))
    }

    struct Signer: Equatable {
        let authority: String
        let team: String
        var description: String { authority.isEmpty ? "unsigned" : authority }
    }

    /// The leaf `Authority=` line and `TeamIdentifier=` from `codesign -dvvv`.
    static func signer(of bundle: URL) -> Signer {
        let result = run("/usr/bin/codesign", ["-dvvv", bundle.path])
        var authority = "", team = ""
        for line in result.stderr.split(whereSeparator: \.isNewline) {
            if authority.isEmpty, line.hasPrefix("Authority=") { authority = String(line.dropFirst(10)) }
            if line.hasPrefix("TeamIdentifier=") { team = String(line.dropFirst(15)) }
        }
        return Signer(authority: authority, team: team)
    }

    /// Swaps the whole bundle so no stale file breaks the signature seal.
    private static func replace(_ installed: URL, with downloaded: URL) throws {
        let target = installed.path
        let script = UpdateInstall.replacementScript(installed: installed, downloaded: downloaded)
        let parent = installed.deletingLastPathComponent().path
        if FileManager.default.isWritableFile(atPath: parent), FileManager.default.isWritableFile(atPath: target) {
            let result = run("/bin/sh", ["-c", script])
            guard result.ok else { throw UpdateError.badDownload(result.stderr) }
        } else {
            try runAsAdmin(script)
        }
    }

    /// Runs one trusted shell command as root through the macOS password dialog.
    /// Only paths that this updater builds may reach it.
    private static func runAsAdmin(_ command: String) throws {
        let escaped = command.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
        var error: NSDictionary?
        _ = NSAppleScript(source: "do shell script \"\(escaped)\" with administrator privileges")?
            .executeAndReturnError(&error)
        guard let error else { return }
        if error[NSAppleScript.errorNumber] as? Int == -128 { throw UpdateError.cancelled }
        throw UpdateError.badDownload(error[NSAppleScript.errorMessage] as? String ?? "install failed")
    }

    /// The running service keeps the old binary. Restart it if it is installed.
    private static func restartService() {
        _ = run("/bin/launchctl", ["kickstart", "-k", "gui/\(getuid())/dev.passlet.agent"])
    }

    private static func relaunch(_ menu: URL) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = ["-c", "sleep 1; /usr/bin/open \(quoted(menu.path))"]
        try? process.run()
        DispatchQueue.main.async { NSApp.terminate(nil) }
    }

    private static func quoted(_ path: String) -> String {
        "'" + path.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    @discardableResult
    private static func run(_ launchPath: String, _ arguments: [String]) -> (ok: Bool, stderr: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: launchPath)
        process.arguments = arguments
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = FileHandle.nullDevice
        let error = Pipe()
        process.standardError = error
        guard (try? process.run()) != nil else { return (false, "cannot run \(launchPath)") }
        let data = error.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return (process.terminationStatus == 0, String(decoding: data, as: UTF8.self))
    }
}
