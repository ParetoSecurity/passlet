import AppKit
import ServiceManagement
import SwiftUI

// Menu bar app for the approving Mac. It runs `passlet serve` unless the login
// service is installed. Approvals still use PassletHelper.
@main
struct PassletMenuApp: App {
    @StateObject private var model = MenuModel()

    var body: some Scene {
        MenuBarExtra {
            MenuContent(model: model)
        } label: {
            HStack(spacing: 2) {
                Image(nsImage: MenuIcon.image)
                if !model.serviceReady {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.system(size: 9))
                }
            }
            .accessibilityLabel(model.serviceReady ? "Passlet" : "Passlet, service unavailable")
        }
    }
}

@MainActor
final class MenuModel: ObservableObject {
    @Published var serviceReady = false
    @Published var account: String?
    @Published var keychain: String?
    @Published var activity: [ServiceProbe.Activity] = []
    @Published var availableUpdate: PassletRelease?
    @Published var updating = false
    @Published var updateMessage: String?
    @Published var startsAtLogin = SMAppService.mainApp.status == .enabled
    @Published var serviceMode: ServiceSupervisor.Mode = .stopped("not started")
    @Published var doctorRunning = false
    @AppStorage("autoCheckUpdates") var autoCheckUpdates = true

    private var timer: Timer?
    private var lastUpdateCheck = Date.distantPast
    private let supervisor = SelfUpdater.installedApp.map(ServiceSupervisor.init)
    private var terminateSignal: DispatchSourceSignal?

    init() {
        supervisor?.start()
        serviceMode = supervisor?.mode ?? .stopped("development build")
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification,
                                               object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.supervisor?.stop() }
        }
        // `kill` and logout send SIGTERM. Quit normally so the service stops too.
        signal(SIGTERM, SIG_IGN)
        terminateSignal = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
        terminateSignal?.setEventHandler { NSApp.terminate(nil) }
        terminateSignal?.resume()
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
    }

    var version: String { SelfUpdater.installedVersion }

    func refresh() {
        let app = SelfUpdater.installedApp
        Task.detached {
            let ready = ServiceProbe.serviceReady()
            let login = ServiceProbe.accountLogin()
            let keychain = app.flatMap { ServiceProbe.keychainStatus(app: $0) }
            let activity = app.map { ServiceProbe.recentActivity(app: $0) } ?? []
            await MainActor.run {
                if ready { self.supervisor?.markHealthy() }
                self.serviceMode = self.supervisor?.mode ?? self.serviceMode
                self.serviceReady = ready
                self.account = login
                self.keychain = keychain
                self.activity = activity
            }
        }
        // Check GitHub at most once a day unless the user asks.
        if autoCheckUpdates, Date().timeIntervalSince(lastUpdateCheck) > 24 * 60 * 60 {
            Task { await checkForUpdate(manual: false) }
        }
    }

    func checkForUpdate(manual: Bool) async {
        lastUpdateCheck = Date()
        do {
            availableUpdate = try await SelfUpdater.check()
            if manual { updateMessage = availableUpdate == nil ? "Passlet \(version) is up to date." : nil }
        } catch {
            if manual { updateMessage = error.localizedDescription }
        }
    }

    func installUpdate() {
        guard let release = availableUpdate, !updating else { return }
        updating = true
        updateMessage = nil
        Task {
            do {
                try await SelfUpdater.installAndRelaunch(release)
            } catch {
                updateMessage = error.localizedDescription
                updating = false
            }
        }
    }

    func toggleStartAtLogin() {
        do {
            if startsAtLogin { try SMAppService.mainApp.unregister() } else { try SMAppService.mainApp.register() }
        } catch {
            updateMessage = "Start at Login failed: \(error.localizedDescription)"
        }
        startsAtLogin = SMAppService.mainApp.status == .enabled
    }

    var serviceSummary: String {
        switch (serviceReady, serviceMode) {
        case (true, .loginService): return "Service: ready (login service)"
        case (true, .running): return "Service: ready"
        case (true, _): return "Service: ready (started elsewhere)"
        case (false, .running): return "Service: starting…"
        case (false, .loginService): return "Service: login service not responding"
        case (false, .restarting(let why)), (false, .stopped(let why)): return "Service: \(why)"
        }
    }

    /// `setup github` waits for the browser flow, so it runs in Terminal.
    func openGitHubSetup() {
        guard let app = SelfUpdater.installedApp else { return }
        let cli = app.appendingPathComponent("Contents/MacOS/passlet").path
        let script = FileManager.default.temporaryDirectory.appendingPathComponent("passlet-setup-github.command")
        let body = "#!/bin/sh\nclear\n'\(cli.replacingOccurrences(of: "'", with: "'\\''"))' setup github\n"
        do {
            try body.write(to: script, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
            NSWorkspace.shared.open(script)
        } catch {
            updateMessage = "Cannot open Terminal: \(error.localizedDescription)"
        }
    }

    func runDoctor() {
        guard let app = SelfUpdater.installedApp, !doctorRunning else { return }
        doctorRunning = true
        Task.detached {
            let result = ServiceProbe.doctor(app: app)
            await MainActor.run {
                self.doctorRunning = false
                let alert = NSAlert()
                alert.messageText = result.ok ? "Passlet doctor found no problems" : "Passlet doctor found problems"
                alert.informativeText = result.output
                alert.alertStyle = result.ok ? .informational : .warning
                NSApp.activate(ignoringOtherApps: true)
                alert.runModal()
            }
        }
    }

    func openServiceLog() {
        let name = serviceMode == .loginService ? "service.log" : "menu-service.log"
        NSWorkspace.shared.open(ServiceProbe.stateDir.appendingPathComponent(name))
    }

    func openSetupGuide() {
        if let app = SelfUpdater.installedApp {
            NSWorkspace.shared.open(app.appendingPathComponent("Contents/Resources/docs/SETUP.md"))
        }
    }
}

struct MenuContent: View {
    @ObservedObject var model: MenuModel

    var body: some View {
        Text(model.serviceSummary)
        Text("GitHub: \(model.account ?? "not set up")")
        if let keychain = model.keychain {
            Text("Keychain: \(keychain.replacingOccurrences(of: "_", with: " "))")
        }
        Menu("Recent Requests") {
            if model.activity.isEmpty {
                Text("No requests yet")
            } else {
                ForEach(model.activity) { Text($0.summary) }
            }
        }
        Divider()
        if let update = model.availableUpdate {
            Button(model.updating ? "Installing \(update.tagName)…" : "Install Passlet \(update.tagName)") {
                model.installUpdate()
            }
            .disabled(model.updating)
        } else {
            Button("Check for Updates…") { Task { await model.checkForUpdate(manual: true) } }
                .disabled(SelfUpdater.installedApp == nil)
        }
        if let message = model.updateMessage {
            Text(message)
        }
        Toggle("Check for Updates Automatically", isOn: $model.autoCheckUpdates)
        Toggle("Start at Login", isOn: Binding(get: { model.startsAtLogin }, set: { _ in model.toggleStartAtLogin() }))
        Divider()
        Button(model.account == nil ? "Set Up GitHub…" : "Set Up GitHub Again…") { model.openGitHubSetup() }
            .disabled(SelfUpdater.installedApp == nil)
        Button(model.doctorRunning ? "Running Doctor…" : "Run Doctor") { model.runDoctor() }
            .disabled(SelfUpdater.installedApp == nil || model.doctorRunning)
        Button("Open Service Log") { model.openServiceLog() }
        Button("Setup Guide") { model.openSetupGuide() }
        Button("Refresh") { model.refresh() }
        Text("Passlet \(model.version)")
        Button("Quit Passlet Menu") { NSApp.terminate(nil) }
            .keyboardShortcut("q")
    }
}
