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
