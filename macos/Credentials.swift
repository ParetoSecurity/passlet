import CoreFoundation
import Foundation

func credentialData(_ value: Any?, provider: String = "github") -> Data? {
    if provider == "cloudflare" {
        guard let c = value as? [String: Any], c["provider"] as? String == "cloudflare",
              let token = c["access_token"] as? String, !token.isEmpty,
              let id = c["cloudflare_token_id"] as? String,
              id.utf8.count == 32, id.range(of: "^[a-f0-9]{32}$", options: .regularExpression) != nil else { return nil }
        return try? JSONSerialization.data(withJSONObject: c)
    }
    guard provider == "github" else { return nil }
    guard let c = value as? [String: Any], c["provider"] == nil || c["provider"] as? String == "",
          ["client_id", "client_secret", "access_token", "refresh_token", "expires_at"].allSatisfy({ (c[$0] as? String)?.isEmpty == false }),
          let account = c["account"] as? [String: Any],
          ["login", "app_url", "verified_at"].allSatisfy({ (account[$0] as? String)?.isEmpty == false }),
          let appNumber = account["app_id"] as? NSNumber,
          CFGetTypeID(appNumber) != CFBooleanGetTypeID(),
          let appID = account["app_id"] as? Int, appID > 0 else { return nil }
    return try? JSONSerialization.data(withJSONObject: c)
}
