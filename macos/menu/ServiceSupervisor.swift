import Foundation

// Runs `passlet serve` while the menu app runs. If the login service is
// installed, launchd owns the service and this supervisor stays idle.
@MainActor
final class ServiceSupervisor {
    enum Mode: Equatable {
        case loginService
        case running
        case restarting(String)
        case stopped(String)
    }

    private(set) var mode: Mode = .stopped("not started")
    private var process: Process?
    private var stopping = false
    private var failures = 0
    private let app: URL

    init(app: URL) { self.app = app }

    static var loginServiceInstalled: Bool {
        let check = Process()
        check.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        check.arguments = ["print", "gui/\(getuid())/dev.passlet.agent"]
        check.standardOutput = FileHandle.nullDevice
        check.standardError = FileHandle.nullDevice
        guard (try? check.run()) != nil else { return false }
        check.waitUntilExit()
        return check.terminationStatus == 0
    }

    func start() {
        stopping = false
        guard process == nil else { return }
        if Self.loginServiceInstalled {
            mode = .loginService
            return
        }
        let state = ServiceProbe.stateDir
        // The service requires a private state directory owned by the user.
        try? FileManager.default.createDirectory(at: state, withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        let log = state.appendingPathComponent("menu-service.log")
        if !FileManager.default.fileExists(atPath: log.path) {
            FileManager.default.createFile(atPath: log.path, contents: nil, attributes: [.posixPermissions: 0o600])
        }
        let serve = Process()
        serve.executableURL = app.appendingPathComponent("Contents/MacOS/passlet")
        serve.arguments = ["serve"]
        serve.standardInput = FileHandle.nullDevice
        if let handle = try? FileHandle(forWritingTo: log) {
            handle.seekToEndOfFile()
            serve.standardOutput = handle
            serve.standardError = handle
        }
        serve.terminationHandler = { [weak self] finished in
            let status = finished.terminationStatus
            Task { @MainActor in self?.exited(status: status) }
        }
        do {
            try serve.run()
            process = serve
            mode = .running
        } catch {
            mode = .stopped("cannot start: \(error.localizedDescription)")
        }
    }

    /// Sends SIGTERM. `passlet serve` closes its socket and exits.
    func stop() {
        stopping = true
        process?.terminate()
        process?.waitUntilExit()
        process = nil
    }

    /// The service ran long enough to answer a health check.
    func markHealthy() { failures = 0 }

    private func exited(status: Int32) {
        process = nil
        guard !stopping else {
            mode = .stopped("stopped")
            return
        }
        failures += 1
        // Back off when the service exits repeatedly, for example when another
        // `passlet serve` already holds the socket lock.
        let delay = min(60.0, 5.0 * Double(failures))
        mode = .restarting("exited with status \(status); restarting in \(Int(delay)) s")
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self, !self.stopping else { return }
            self.start()
        }
    }
}
