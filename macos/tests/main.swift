import Foundation

func check(_ value: @autoclosure () -> Bool, _ message: String) {
    if !value() { fputs("FAIL: \(message)\n", stderr); exit(1) }
}

func test(_ name: String, _ body: () throws -> Void) {
    do {
        try body()
        print("PASS: \(name)")
    } catch {
        fputs("FAIL: \(name): \(error)\n", stderr)
        exit(1)
    }
}

let credentials: [String: Any] = [
    "client_id": "fixture-client",
    "client_secret": "fixture-secret",
    "access_token": "fixture-access",
    "refresh_token": "fixture-refresh",
    "expires_at": "2026-11-04T04:00:00Z",
    "account": [
        "login": "alice",
        "app_id": 123,
        "app_url": "https://github.com/apps/fixture",
        "verified_at": "2026-11-03T20:00:00Z",
    ],
]

test("credentials round-trip without losing account metadata") {
    let data = credentialData(credentials)
    check(data != nil, "valid credentials rejected")
    let decoded = try JSONSerialization.jsonObject(with: data!) as? NSDictionary
    check(decoded == credentials as NSDictionary, "credential contents changed")
}

test("credentials require nonempty strings and a verified account") {
    for value: Any? in [nil, NSNull(), "text", [], 42] {
        check(credentialData(value) == nil, "accepted non-object credentials")
    }
    for key in ["client_id", "client_secret", "access_token", "refresh_token", "expires_at"] {
        for value: Any? in [nil, NSNull(), "", 42, true, []] {
            var invalid = credentials
            invalid[key] = value
            check(credentialData(invalid) == nil, "accepted invalid \(key)")
        }
    }
    for value: Any? in [nil, NSNull(), "account", [], 42] {
        var invalid = credentials
        invalid["account"] = value
        check(credentialData(invalid) == nil, "accepted non-object account")
    }
    for key in ["login", "app_url", "verified_at", "app_id"] {
        let invalidValues: [Any?] = key == "app_id" ? [nil, NSNull(), "123", 0, -1, 1.5, true] : [nil, NSNull(), "", 42, []]
        for value in invalidValues {
            var invalid = credentials
            var account = credentials["account"] as! [String: Any]
            account[key] = value
            invalid["account"] = account
            let wireData = try JSONSerialization.data(withJSONObject: invalid)
            let wireCredentials = try JSONSerialization.jsonObject(with: wireData)
            check(credentialData(wireCredentials) == nil, "accepted invalid account \(key)")
        }
    }
}

test("approval prompt boundaries and retry after invalid input") {
    var state = ApprovalState()
    check(!state.authorize(), "authorization before request")
    check(!state.consumeRotation(), "rotation before authentication")
    check(!state.begin(prompt: ""), "empty prompt")
    check(!state.begin(prompt: String(repeating: "x", count: 1001)), "oversized prompt")
    check(!state.started && !state.finishing && !state.authorized, "invalid input changed state")
    check(state.begin(prompt: String(repeating: "x", count: 1000)), "boundary prompt rejected")
    check(!state.begin(prompt: "second request"), "second approval in same helper")
    check(!state.consumeRotation(), "rotation before authentication")
    check(state.authorize(), "successful authentication")
    check(state.consumeRotation(), "first rotation")
    check(!state.consumeRotation(), "second rotation")
    check(state.finish(), "completion")
    check(!state.begin(prompt: "new request"), "request after completion")
    check(!state.authorize(), "late callback after completion")
    check(!state.finish(), "duplicate completion")
}

for phase in ["before request", "awaiting authentication", "authorized", "rotated"] {
    test("completion is terminal \(phase)") {
        var state = ApprovalState()
        if phase != "before request" { check(state.begin(prompt: "fixture request"), "begin") }
        if phase == "authorized" || phase == "rotated" { check(state.authorize(), "authorize") }
        if phase == "rotated" { check(state.consumeRotation(), "rotate") }
        check(state.finish(), "completion")
        check(!state.authorized, "completion retained authorization")
        check(!state.begin(prompt: "late request"), "late request accepted")
        check(!state.authorize(), "late authentication accepted")
        check(!state.consumeRotation(), "late mutation accepted")
        check(!state.finish(), "duplicate completion")
    }
}

check(CommandLine.arguments.count == 2, "expected the compiled helper path")
let helperURL = URL(fileURLWithPath: CommandLine.arguments[1])

func runHelper(arguments: [String] = [], input: String?, expectedError: String?, expectedStatus: Int32 = 0) throws {
    let process = Process()
    process.executableURL = helperURL
    process.arguments = arguments
    let stdin = Pipe()
    let stdout = Pipe()
    let stderr = Pipe()
    process.standardInput = input == nil ? FileHandle.nullDevice : stdin.fileHandleForReading
    process.standardOutput = stdout
    process.standardError = stderr
    let exited = DispatchSemaphore(value: 0)
    process.terminationHandler = { _ in exited.signal() }
    try process.run()
    if let input = input {
        try stdin.fileHandleForWriting.write(contentsOf: Data(input.utf8))
    }
    try stdin.fileHandleForWriting.close()
    if exited.wait(timeout: .now() + 10) == .timedOut {
        process.terminate()
        check(false, "helper did not exit after invalid input or EOF")
    }
    check(process.terminationStatus == expectedStatus, "unexpected helper exit status")
    let output = stdout.fileHandleForReading.readDataToEndOfFile()
    let errors = stderr.fileHandleForReading.readDataToEndOfFile()
    if let expectedError = expectedError {
        let response = try JSONSerialization.jsonObject(with: output) as? NSDictionary
        check(response == ["ok": false, "error": expectedError] as NSDictionary, "unexpected helper response")
        check(!String(decoding: errors, as: UTF8.self).contains("fixture-secret"), "credentials leaked to stderr")
    } else {
        check(output.isEmpty, "unexpected helper response")
    }
    if input == nil {
        check(String(decoding: errors, as: UTF8.self).contains("Use the passlet CLI"), "interactive launch did not explain CLI usage")
    }
}

test("helper rejects non-pipe stdin") {
    try runHelper(input: nil, expectedError: nil, expectedStatus: 1)
}

test("helper exits cleanly when its parent closes stdin") {
    try runHelper(input: "", expectedError: nil)
}

for input in ["not json", "[]", "null", "{}", "{\"op\":42}", "{\"op\":\"unknown\"}", String(repeating: "x", count: 65537)] {
    test("helper rejects malformed or unknown requests (\(input.utf8.count) bytes)") {
        try runHelper(input: input + "\n", expectedError: "invalid_request")
    }
}

for prompt: Any? in [nil, NSNull(), 42, "", String(repeating: "x", count: 1001)] {
    test("helper rejects invalid approval prompts before authentication") {
        var message: [String: Any] = ["op": "approve"]
        message["prompt"] = prompt
        let data = try JSONSerialization.data(withJSONObject: message)
        try runHelper(input: String(decoding: data, as: UTF8.self) + "\n", expectedError: "invalid_request")
    }
}

for operation in ["store", "delete"] {
    test("helper rejects \(operation) before approval") {
        let data = try JSONSerialization.data(withJSONObject: ["op": operation, "credentials": credentials])
        try runHelper(input: String(decoding: data, as: UTF8.self) + "\n", expectedError: "invalid_request")
    }
}

test("helper emits only one error for queued invalid requests") {
    try runHelper(input: "{\"op\":\"unknown\"}\n{\"op\":\"delete\"}\n", expectedError: "invalid_request")
}

for input in ["", "not json\n", "[]\n", "{\"credentials\":{}}\n", String(repeating: "x", count: 65537) + "\n"] {
    test("helper rejects invalid imports before accessing Keychain") {
        try runHelper(arguments: ["--import"], input: input, expectedError: "invalid_credentials", expectedStatus: 1)
    }
}
