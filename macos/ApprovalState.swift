import Foundation

// Completion is terminal: late authentication callbacks must never authorize
// a Keychain read or rotation after cancellation/disconnect.
struct ApprovalState {
    private(set) var started = false
    private(set) var finishing = false
    private(set) var authorized = false

    mutating func begin(prompt: String) -> Bool {
        guard !started, !finishing, !prompt.isEmpty, prompt.count <= 1000 else { return false }
        started = true
        return true
    }

    mutating func authorize() -> Bool {
        guard started, !finishing else { return false }
        authorized = true
        return true
    }

    mutating func consumeRotation() -> Bool {
        guard authorized, !finishing else { return false }
        authorized = false
        return true
    }

    mutating func finish() -> Bool {
        guard !finishing else { return false }
        finishing = true
        authorized = false
        return true
    }
}
