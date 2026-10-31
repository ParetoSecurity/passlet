import Foundation

func check(_ value: @autoclosure () -> Bool, _ message: String) {
    if !value() { fputs("FAIL: \(message)\n", stderr); exit(1) }
}

var state = ApprovalState()
check(!state.authorize(), "authorization before request")
check(!state.consumeRotation(), "rotation before authentication")
check(!state.begin(prompt: ""), "empty prompt")
check(!state.begin(prompt: String(repeating: "x", count: 1001)), "oversized prompt")
check(state.begin(prompt: "grant local/SSH socket client GitHub access\nRepositories:\n• acme/test"), "valid prompt")
check(!state.begin(prompt: "second request"), "second approval in same helper")
check(state.authorize(), "successful authentication")
check(state.consumeRotation(), "first rotation")
check(!state.consumeRotation(), "second rotation")
check(state.finish(), "completion")
check(!state.authorize(), "late callback after completion")
check(!state.finish(), "duplicate completion")

for reason in ["denial", "timeout", "disconnect"] {
    var cancelled = ApprovalState()
    check(cancelled.begin(prompt: "test request"), "begin \(reason)")
    check(cancelled.finish(), "cancel \(reason)")
    check(!cancelled.authorize(), "late successful callback after \(reason)")
    check(!cancelled.consumeRotation(), "credential rotation after \(reason)")
}
print("PASS: native helper lifecycle, cancellation, and one-time rotation")
