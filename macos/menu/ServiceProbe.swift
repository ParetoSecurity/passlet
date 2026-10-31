import Foundation

// Read-only probes for the menu. They never unlock credentials, mint tokens or
// start the service. The launch agent owns `passlet serve`.
enum ServiceProbe {
    static var stateDir: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".local/share/passlet")
    }

    /// Sends `GET /v1/health` over the private Unix socket.
    static func serviceReady() -> Bool {
        let path = stateDir.appendingPathComponent("passlet.sock").path
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { return false }
        defer { close(fd) }
        var timeout = timeval(tv_sec: 2, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let capacity = MemoryLayout.size(ofValue: address.sun_path)
        guard path.utf8.count < capacity else { return false }
        withUnsafeMutableBytes(of: &address.sun_path) { buffer in
            buffer.copyBytes(from: path.utf8)
            buffer[path.utf8.count] = 0
        }
        let connected = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard connected == 0 else { return false }
        let request = "GET /v1/health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
        guard request.withCString({ send(fd, $0, strlen($0), 0) }) > 0 else { return false }
        var response = Data()
        var chunk = [UInt8](repeating: 0, count: 1024)
        while response.count < 4096 {
            let count = recv(fd, &chunk, chunk.count, 0)
            if count <= 0 { break }
            response.append(chunk, count: count)
        }
        return healthy(String(decoding: response, as: UTF8.self))
    }

    static func healthy(_ response: String) -> Bool {
        guard response.hasPrefix("HTTP/1.1 200"),
              let body = response.range(of: "\r\n\r\n").map({ response[$0.upperBound...] }),
              let object = try? JSONSerialization.jsonObject(with: Data(body.utf8)) as? [String: Any] else {
            return false
        }
        return object["status"] as? String == "ready"
    }

    /// GitHub login from setup metadata. It is local, not freshly verified.
    static func accountLogin() -> String? {
        let url = stateDir.appendingPathComponent("account.json")
        guard let data = try? Data(contentsOf: url),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        return object["login"] as? String
    }

    /// Runs `PassletHelper --status`. The helper reads only item attributes.
    static func keychainStatus(app: URL) -> String? {
        let output = run(app.appendingPathComponent("Contents/MacOS/PassletHelper"), ["--status"])
        guard let output,
              let object = try? JSONSerialization.jsonObject(with: Data(output.utf8)) as? [String: Any] else { return nil }
        return object["status"] as? String
    }

    struct Activity: Identifiable {
        let id: String
        let summary: String
    }

    /// Recent audit events from `passlet audit`, newest first.
    static func recentActivity(app: URL, limit: Int = 5) -> [Activity] {
        guard let output = run(app.appendingPathComponent("Contents/MacOS/passlet"), ["audit", "--limit", String(limit)]),
              let entries = try? JSONSerialization.jsonObject(with: Data(output.utf8)) as? [[String: Any]] else { return [] }
        return entries.enumerated().map { index, entry in
            let event = entry["event"] as? [String: Any] ?? [:]
            let requested = event["requested"] as? [String: Any] ?? [:]
            let repositories = (requested["repositories"] as? [String] ?? []).joined(separator: ", ")
            let outcome = (event["outcome"] as? String ?? "unknown").replacingOccurrences(of: "_", with: " ")
            let at = (entry["at"] as? String).flatMap(relativeTime) ?? ""
            let id = event["request_id"] as? String ?? String(index)
            return Activity(id: id, summary: [outcome, repositories, at].filter { !$0.isEmpty }.joined(separator: " · "))
        }
    }

    private static func relativeTime(_ value: String) -> String? {
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = parser.date(from: value) ?? ISO8601DateFormatter().date(from: value) else { return nil }
        return RelativeDateTimeFormatter().localizedString(for: date, relativeTo: Date())
    }

    private static func run(_ executable: URL, _ arguments: [String]) -> String? {
        let process = Process()
        process.executableURL = executable
        process.arguments = arguments
        process.standardInput = FileHandle.nullDevice
        let output = Pipe()
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        guard (try? process.run()) != nil else { return nil }
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else { return nil }
        return String(decoding: data, as: UTF8.self)
    }
}
