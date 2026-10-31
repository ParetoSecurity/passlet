import Foundation

// Pure release selection for the menu bar updater. No network or AppKit, so
// the check script can test it without a signed bundle.
struct ReleaseAsset: Decodable, Equatable {
    let name: String
    let browserDownloadUrl: URL
    let size: Int
}

struct PassletRelease: Decodable, Equatable {
    let tagName: String
    let htmlUrl: String
    let prerelease: Bool
    let draft: Bool
    let assets: [ReleaseAsset]

    var version: [Int] { UpdateCheck.parseVersion(tagName) }
    var appAsset: ReleaseAsset? {
        assets.first { $0.name == "passlet_\(tagName.dropFirst())_macos_universal.zip" }
    }
    var checksumAsset: ReleaseAsset? {
        assets.first { $0.name == "passlet_\(tagName.dropFirst())_SHA256SUMS" }
    }
}

enum UpdateCheck {
    static let repo = "ParetoSecurity/passlet"
    static let releasesURL = URL(string: "https://api.github.com/repos/\(repo)/releases?per_page=10")!

    static func decodeReleases(_ data: Data) throws -> [PassletRelease] {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return try decoder.decode([PassletRelease].self, from: data)
    }

    /// Returns the newest published release that is newer than `installed`.
    /// The release must ship the macOS archive and its checksum file.
    static func newest(_ releases: [PassletRelease], installed: String) -> PassletRelease? {
        let current = parseVersion(installed)
        guard !current.isEmpty else { return nil }
        return releases
            .filter { !$0.draft && !$0.prerelease && $0.appAsset != nil && $0.checksumAsset != nil }
            .filter { isNewer($0.version, than: current) }
            .max { isNewer($1.version, than: $0.version) }
    }

    /// Finds the SHA-256 for `name` in a `shasum -a 256` listing.
    static func checksum(for name: String, in listing: String) -> String? {
        for line in listing.split(whereSeparator: \.isNewline) {
            let parts = line.split(separator: " ", maxSplits: 1)
            guard parts.count == 2 else { continue }
            let file = parts[1].trimmingCharacters(in: CharacterSet(charactersIn: " *"))
            let digest = parts[0].lowercased()
            if file == name, digest.count == 64, digest.allSatisfy(\.isHexDigit) { return digest }
        }
        return nil
    }

    /// "v1.2.3" and "1.2.3" give [1, 2, 3]. Other values give [].
    static func parseVersion(_ tag: String) -> [Int] {
        let core = tag.hasPrefix("v") ? String(tag.dropFirst()) : tag
        let parts = core.split(separator: ".", omittingEmptySubsequences: false)
        let numbers = parts.compactMap { Int($0) }
        return parts.count == 3 && numbers.count == 3 ? numbers : []
    }

    static func isNewer(_ a: [Int], than b: [Int]) -> Bool {
        for (x, y) in zip(a, b) where x != y { return x > y }
        return a.count > b.count
    }
}
