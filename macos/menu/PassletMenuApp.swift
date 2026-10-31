import AppKit
import ServiceManagement
import SwiftUI

// Optional menu bar status for the approving Mac. The launch agent still runs
// the service, and approvals still use PassletHelper.
@main
struct PassletMenuApp: App {
    @StateObject private var model = MenuModel()

    var body: some Scene {
        MenuBarExtra {
            MenuContent(model: model)
        } label: {
            Image(systemName: model.serviceReady ? "lock.shield" : "lock.trianglebadge.exclamationmark")
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
    @AppStorage("autoCheckUpdates") var autoCheckUpdates = true

    private var timer: Timer?
    private var lastUpdateCheck = Date.distantPast

    init() {
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

    func openSetupGuide() {
        if let app = SelfUpdater.installedApp {
            NSWorkspace.shared.open(app.appendingPathComponent("Contents/Resources/docs/SETUP.md"))
        }
    }
}

struct MenuContent: View {
    @ObservedObject var model: MenuModel

    var body: some View {
        Text(model.serviceReady ? "Service: ready" : "Service: not running")
        Text("GitHub: \(model.account ?? "not set up")")
        if let keychain = model.keychain {
            Text("Keychain: \(keychain.replacingOccurrences(of: "_", with: " "))")
        }
        if !model.serviceReady {
            Text("Run passlet service install in Terminal.")
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
        Button("Setup Guide") { model.openSetupGuide() }
        Button("Refresh") { model.refresh() }
        Text("Passlet \(model.version)")
        Button("Quit Passlet Menu") { NSApp.terminate(nil) }
            .keyboardShortcut("q")
    }
}
