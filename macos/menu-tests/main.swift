import Foundation

func check(_ value: @autoclosure () -> Bool, _ message: String) {
    if !value() { fputs("FAIL: \(message)\n", stderr); exit(1) }
}

func release(_ tag: String, draft: Bool = false, prerelease: Bool = false, assets: [String]? = nil) -> PassletRelease {
    let version = tag.dropFirst()
    let names = assets ?? ["passlet_\(version)_macos_universal.zip", "passlet_\(version)_SHA256SUMS"]
    return PassletRelease(
        tagName: tag, htmlUrl: "https://github.com/ParetoSecurity/passlet/releases/tag/\(tag)",
        prerelease: prerelease, draft: draft,
        assets: names.map { ReleaseAsset(name: $0, browserDownloadUrl: URL(string: "https://example.invalid/\($0)")!, size: 1) })
}

check(UpdateCheck.parseVersion("v1.2.3") == [1, 2, 3], "tag with v prefix")
check(UpdateCheck.parseVersion("0.2.0") == [0, 2, 0], "bundle version")
check(UpdateCheck.parseVersion("v1.2").isEmpty, "two-part version")
check(UpdateCheck.parseVersion("v1.2.3-rc1").isEmpty, "pre-release suffix")
check(UpdateCheck.parseVersion("dev").isEmpty, "development build")
check(UpdateCheck.isNewer([0, 10, 0], than: [0, 9, 9]), "numeric, not lexical, comparison")
check(!UpdateCheck.isNewer([0, 2, 0], than: [0, 2, 0]), "same version")

let releases = [release("v0.1.0"), release("v0.3.0", draft: true), release("v0.4.0", prerelease: true),
                release("v0.2.1"), release("v0.2.5", assets: ["passlet_0.2.5_macos_universal.zip"]),
                release("v0.2.2", assets: ["passlet_0.2.2_linux_amd64.tar.gz", "passlet_0.2.2_SHA256SUMS"])]
check(UpdateCheck.newest(releases, installed: "0.2.0")?.tagName == "v0.2.1", "skips drafts, pre-releases and incomplete releases")
check(UpdateCheck.newest(releases, installed: "0.2.1") == nil, "already up to date")
check(UpdateCheck.newest(releases, installed: "dev") == nil, "development builds never update")
check(release("v0.2.1").appAsset?.name == "passlet_0.2.1_macos_universal.zip", "archive asset name")

let digest = String(repeating: "ab", count: 32)
let listing = "\(digest)  passlet_0.2.1_macos_universal.zip\n\(String(repeating: "cd", count: 32))  passlet_0.2.1_linux_amd64.tar.gz\n"
check(UpdateCheck.checksum(for: "passlet_0.2.1_macos_universal.zip", in: listing) == digest, "checksum lookup")
check(UpdateCheck.checksum(for: "passlet_0.2.1_macos_universal.zip", in: "short  passlet_0.2.1_macos_universal.zip") == nil, "rejects malformed digest")
check(UpdateCheck.checksum(for: "missing.zip", in: listing) == nil, "missing archive")

let api = Data("""
[{"tag_name":"v0.2.1","html_url":"https://github.com/ParetoSecurity/passlet/releases/tag/v0.2.1","prerelease":false,"draft":false,
  "assets":[{"name":"passlet_0.2.1_macos_universal.zip","browser_download_url":"https://example.invalid/a.zip","size":10}]}]
""".utf8)
check((try? UpdateCheck.decodeReleases(api))?.first?.appAsset?.size == 10, "GitHub API decoding")
let ready = "HTTP/1.1 200 OK\r\nCache-Control: no-store\r\nContent-Type: application/json\r\nContent-Length: 18\r\nConnection: close\r\n\r\n{\"status\":\"ready\"}"
check(ServiceProbe.healthy(ready), "real passlet serve health response")
check(!ServiceProbe.healthy(ready.replacingOccurrences(of: "200 OK", with: "403 Forbidden")), "rejected caller")
check(!ServiceProbe.healthy("HTTP/1.1 200 OK\r\n\r\n{\"status\":\"starting\"}"), "not ready")
check(!ServiceProbe.healthy(""), "no response")
print("PASS: menu update selection, checksum lookup and service health parsing")

let fm = FileManager.default
let root = fm.temporaryDirectory.appendingPathComponent("passlet-update-tests-\(UUID().uuidString)")
try fm.createDirectory(at: root, withIntermediateDirectories: true)
defer { try? fm.removeItem(at: root) }

func makeApp(_ app: URL, version: String, identifier: String = "dev.passlet.app") throws {
    for (path, executable, bundleID) in [("Contents", "PassletHelper", identifier),
                                        ("Contents/Helpers/PassletMenu.app/Contents", "PassletMenu", identifier + ".menu")] {
        let contents = app.appendingPathComponent(path)
        try fm.createDirectory(at: contents.appendingPathComponent("MacOS"), withIntermediateDirectories: true)
        let info = ["CFBundleIdentifier": bundleID, "CFBundleExecutable": executable,
                    "CFBundlePackageType": "APPL", "CFBundleVersion": version, "CFBundleShortVersionString": version]
        try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0).write(to: contents.appendingPathComponent("Info.plist"))
        for name in executable == "PassletHelper" ? [executable, "passlet"] : [executable] {
            let file = contents.appendingPathComponent("MacOS/\(name)")
            try Data("#!/bin/sh\nexit 0\n".utf8).write(to: file)
            try fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: file.path)
        }
    }
}

let current = root.appendingPathComponent("Current.app")
let next = root.appendingPathComponent("Next.app")
try makeApp(current, version: "0.1.0")
try makeApp(next, version: "0.2.0")
try UpdateInstall.validate(installed: current, downloaded: next, release: release("v0.2.0"))
func rejects(_ body: () throws -> Void, _ message: String) {
    do { try body(); check(false, message) } catch {}
}
rejects({ try UpdateInstall.validate(installed: current, downloaded: next, release: release("v0.3.0")) }, "reject mismatched release")
try makeApp(next, version: "0.2.0", identifier: "dev.other.app")
rejects({ try UpdateInstall.validate(installed: current, downloaded: next, release: release("v0.2.0")) }, "reject same-signer different app")
try makeApp(next, version: "0.1.0")
rejects({ try UpdateInstall.validate(installed: current, downloaded: next, release: release("v0.1.0")) }, "reject replay")
try makeApp(next, version: "0.0.9")
rejects({ try UpdateInstall.validate(installed: current, downloaded: next, release: release("v0.0.9")) }, "reject downgrade")
try makeApp(next, version: "0.2.0")
try fm.removeItem(at: next.appendingPathComponent("Contents/MacOS/passlet"))
rejects({ try UpdateInstall.validate(installed: current, downloaded: next, release: release("v0.2.0")) }, "reject incomplete bundle")

func replacement(_ name: String, failMove: Bool = false, failRestore: Bool = false, failCopy: Bool = false, existingBackup: Bool = false, existingLock: Bool = false) throws {
    let directory = root.appendingPathComponent(name + " ' quoted")
    try fm.createDirectory(at: directory, withIntermediateDirectories: true)
    let target = directory.appendingPathComponent("Passlet.app")
    let source = directory.appendingPathComponent("Downloaded.app")
    let backup = URL(fileURLWithPath: target.path + ".previous")
    try makeApp(target, version: "0.1.0")
    try makeApp(source, version: "0.2.0")
    if existingBackup { try makeApp(backup, version: "0.0.9") }
    if existingLock { try fm.createDirectory(atPath: target.path + ".update-lock", withIntermediateDirectories: false) }
    var script = UpdateInstall.replacementScript(installed: target, downloaded: source)
    // Fault injection is confined to this test executable, never a production option.
    if failMove {
        script = script.replacingOccurrences(of: "/bin/mv \"$staged\" \"$target\"", with: "/usr/bin/false")
    }
    if failRestore {
        script = script.replacingOccurrences(of: "/bin/mv \"$backup\" \"$target\"", with: "/usr/bin/false")
    }
    if failCopy {
        script = script.replacingOccurrences(of: "/usr/bin/ditto \"$source\" \"$staged\"", with: "/usr/bin/false")
    }
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/bin/sh")
    process.arguments = ["-c", script]
    try process.run()
    process.waitUntilExit()
    let fails = failMove || failCopy || existingBackup || existingLock
    check((process.terminationStatus != 0) == fails, "replacement status: \(name)")
    if failRestore {
        check(!fm.fileExists(atPath: target.path), "failed restore leaves no incomplete target")
        check(fm.fileExists(atPath: backup.appendingPathComponent("Contents/Info.plist").path), "failed restore preserves previous app")
        return
    }
    let info = try Data(contentsOf: target.appendingPathComponent("Contents/Info.plist"))
    let metadata = try PropertyListSerialization.propertyList(from: info, format: nil) as! [String: String]
    check(metadata["CFBundleVersion"] == (fails ? "0.1.0" : "0.2.0"), "old app restored or new app installed: \(name)")
    check(fm.fileExists(atPath: backup.path) == existingBackup, "never destroy an earlier backup: \(name)")
    check(fm.fileExists(atPath: target.path + ".update-lock") == existingLock, "clean own lock only: \(name)")
}
try replacement("success")
try replacement("failed second move", failMove: true)
try replacement("failed recovery", failMove: true, failRestore: true)
try replacement("failed staging", failCopy: true)
try replacement("interrupted earlier update", existingBackup: true)
try replacement("concurrent update", existingLock: true)
print("PASS: update identity, version, executable checks and replacement recovery")
