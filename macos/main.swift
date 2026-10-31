import AppKit
import LocalAuthentication
import Security

// JSON travels only through private parent/child pipes. Never log request bodies,
// Keychain data, provider credentials, or helper replies to a file.
let service = "dev.passlet.github-master.v1"
func query() -> [String: Any] {
    [kSecClass as String: kSecClassGenericPassword,
     kSecAttrService as String: service,
     kSecAttrAccount as String: "github",
     kSecUseDataProtectionKeychain as String: true]
}
func reply(_ value: [String: Any]) {
    guard let data = try? JSONSerialization.data(withJSONObject: value),
          let line = String(data: data, encoding: .utf8) else { exit(1) }
    print(line)
    fflush(stdout)
}
func credentialData(_ value: Any?) -> Data? {
    guard let c = value as? [String: Any],
          ["client_id", "client_secret", "access_token", "refresh_token", "expires_at"].allSatisfy({ (c[$0] as? String)?.isEmpty == false }) else { return nil }
    return try? JSONSerialization.data(withJSONObject: c)
}
if CommandLine.arguments.contains("--status") {
    let context = LAContext()
    context.interactionNotAllowed = true
    var q = query()
    q[kSecReturnAttributes as String] = true
    q[kSecMatchLimit as String] = kSecMatchLimitOne
    q[kSecUseAuthenticationContext as String] = context
    let status = SecItemCopyMatching(q as CFDictionary, nil)
    context.invalidate()
    // Some OS versions protect attributes too. Do not unlock the item to
    // disambiguate, and never treat interactionNotAllowed as proof of access.
    reply(["status": status == errSecSuccess ? "configured" :
        (status == errSecItemNotFound ? "missing" : "protected_or_unavailable")])
    exit(0)
}
if CommandLine.arguments.contains("--import") {
    guard let line = readLine(), line.utf8.count <= 65536,
          let msg = try? JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any],
          let data = credentialData(msg["credentials"]),
          let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, .userPresence, nil) else {
        reply(["ok": false, "error": "invalid_credentials"]); exit(1)
    }
    var q = query()
    q[kSecAttrAccessControl as String] = access
    q[kSecValueData as String] = data
    // Never replace an existing item or weaken its ACL during import.
    let status = SecItemAdd(q as CFDictionary, nil)
    if status == errSecSuccess { reply(["ok": true]) }
    else { reply(["ok": false, "error": status == errSecDuplicateItem ? "credentials_already_exist" : "keychain_import_failed", "status": status]) }
    exit(status == errSecSuccess ? 0 : 1)
}

final class Helper: NSObject, NSApplicationDelegate {
    var context: LAContext?
    var state = ApprovalState()
    func applicationDidFinishLaunching(_ notification: Notification) {
        DispatchQueue.global().async { [self] in
            while let line = readLine() {
                guard line.utf8.count <= 65536,
                      let message = try? JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any] else {
                    DispatchQueue.main.async { self.finish("invalid_request") }; return
                }
                DispatchQueue.main.async { self.handle(message) }
            }
            DispatchQueue.main.async { self.finish(nil) }
        }
        // Independent fail-closed lifetime, even if the parent fails to cancel.
        DispatchQueue.main.asyncAfter(deadline: .now() + 120) { self.finish("approval_cancelled") }
    }
    func finish(_ error: String?) {
        guard state.finish() else { return }
        context?.invalidate()
        if let error = error { reply(["ok": false, "error": error]) }
        NSApp.terminate(nil)
    }
    func handle(_ msg: [String: Any]) {
        guard !state.finishing else { return }
        switch msg["op"] as? String {
        case "approve":
            guard let prompt = msg["prompt"] as? String, state.begin(prompt: prompt) else { finish("invalid_request"); return }
            let ctx = LAContext()
            ctx.localizedCancelTitle = "Deny"
            ctx.touchIDAuthenticationAllowableReuseDuration = 0
            context = ctx
            ctx.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: prompt) { [self] success, error in
                DispatchQueue.main.async {
                    guard !self.state.finishing else { return }
                    guard success else {
                        let denied = (error as? LAError)?.code == .userCancel
                        self.finish(denied ? "approval_denied" : "approval_cancelled"); return
                    }
                    ctx.interactionNotAllowed = true
                    DispatchQueue.global().async {
                        var q = query()
                        q[kSecReturnData as String] = true
                        q[kSecMatchLimit as String] = kSecMatchLimitOne
                        q[kSecUseAuthenticationContext as String] = ctx
                        var result: CFTypeRef?
                        let status = SecItemCopyMatching(q as CFDictionary, &result)
                        let credentials = (result as? Data).flatMap { try? JSONSerialization.jsonObject(with: $0) }
                        DispatchQueue.main.async {
                            guard !self.state.finishing else { return }
                            guard status == errSecSuccess, let credentials = credentials else {
                                self.finish(status == errSecItemNotFound ? "credentials_missing" : "keychain_unavailable"); return
                            }
                            guard self.state.authorize() else { return }
                            reply(["ok": true, "credentials": credentials])
                        }
                    }
                }
            }
        case "store":
            guard let ctx = context, let data = credentialData(msg["credentials"]), state.consumeRotation() else { finish("invalid_request"); return }
            DispatchQueue.global().async {
                var q = query()
                q[kSecUseAuthenticationContext as String] = ctx
                let status = SecItemUpdate(q as CFDictionary, [kSecValueData as String: data] as CFDictionary)
                DispatchQueue.main.async {
                    guard !self.state.finishing else { return }
                    if status == errSecSuccess { reply(["ok": true]) }
                    else { self.finish("keychain_unavailable") }
                }
            }
        default: finish("invalid_request")
        }
    }
    func applicationWillTerminate(_ notification: Notification) { context?.invalidate() }
}
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let helper = Helper()
app.delegate = helper
app.run()
