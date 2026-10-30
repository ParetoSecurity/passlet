import AppKit
import ApplicationServices

func skipped(_ reason: String) -> Never { print("SKIP: \(reason)"); exit(77) }
guard CommandLine.arguments.count == 2 else { skipped("Pass the absolute path to the signed PassletHelper") }
guard AXIsProcessTrusted() else { skipped("Accessibility permission is required for this test runner; no permission prompt requested") }

let helperPath = CommandLine.arguments[1]
let verify = Process()
verify.executableURL = URL(fileURLWithPath: "/usr/bin/codesign")
verify.arguments = ["--verify", "--strict", helperPath]
verify.standardOutput = FileHandle.nullDevice
verify.standardError = FileHandle.nullDevice
do { try verify.run(); verify.waitUntilExit() } catch { skipped("codesign unavailable") }
guard verify.terminationStatus == 0 else { skipped("Helper signature could not be verified") }

let marker = "Passlet UI smoke cancellation \(UUID().uuidString)"
func containsMarker(_ element: AXUIElement, depth: Int = 0) -> Bool {
    if depth > 12 { return false }
    for key in [kAXTitleAttribute, kAXDescriptionAttribute, kAXValueAttribute] {
        var value: CFTypeRef?
        if AXUIElementCopyAttributeValue(element, key as CFString, &value) == .success,
           let text = value as? String, text.contains(marker) { return true }
    }
    var value: CFTypeRef?
    if AXUIElementCopyAttributeValue(element, kAXChildrenAttribute as CFString, &value) == .success,
       let children = value as? [AXUIElement] {
        return children.prefix(200).contains { containsMarker($0, depth: depth + 1) }
    }
    return false
}
func promptVisible() -> Bool {
    for app in NSWorkspace.shared.runningApplications {
        // Only authentication UI and Passlet processes; do not inspect other
        // applications' text or collect screenshots of the user's desktop.
        let name = app.localizedName ?? ""
        if name.contains("Auth") || name.contains("SecurityAgent") || name.contains("Passlet") {
            let element = AXUIElementCreateApplication(app.processIdentifier)
            AXUIElementSetMessagingTimeout(element, 0.2)
            if containsMarker(element) { return true }
        }
    }
    return false
}
let helper = Process()
let input = Pipe()
let output = Pipe()
helper.executableURL = URL(fileURLWithPath: helperPath)
helper.standardInput = input
helper.standardOutput = output
helper.standardError = FileHandle.nullDevice
// Discard all replies, including credentials if a human unexpectedly approves.
output.fileHandleForReading.readabilityHandler = { handle in _ = handle.availableData }
do { try helper.run() } catch { skipped("Helper could not launch in this GUI session") }
let request = try! JSONSerialization.data(withJSONObject: ["op": "approve", "prompt": marker])
input.fileHandleForWriting.write(request + Data([10]))
var visible = false
for _ in 0..<40 {
    if promptVisible() { visible = true; break }
    Thread.sleep(forTimeInterval: 0.2)
}
input.fileHandleForWriting.closeFile()
for _ in 0..<25 {
    if !helper.isRunning && !promptVisible() { break }
    Thread.sleep(forTimeInterval: 0.2)
}
let exited = !helper.isRunning
let dismissed = !promptVisible()
if helper.isRunning { helper.terminate() }
output.fileHandleForReading.readabilityHandler = nil
guard visible else { skipped("System authentication text was not observable; native prompt rendering remains manual") }
guard exited && dismissed else { print("FAIL: authentication UI did not cancel after disconnect"); exit(1) }
print("PASS: native prompt text appeared and cancellation dismissed it. Biometric approval was not tested.")
