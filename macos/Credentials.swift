import CoreFoundation
import Foundation

func credentialData(_ value: Any?) -> Data? {
    guard let c = value as? [String: Any],
          ["client_id", "client_secret", "access_token", "refresh_token", "expires_at"].allSatisfy({ (c[$0] as? String)?.isEmpty == false }),
          let account = c["account"] as? [String: Any],
          ["login", "app_url", "verified_at"].allSatisfy({ (account[$0] as? String)?.isEmpty == false }),
          let appNumber = account["app_id"] as? NSNumber,
          CFGetTypeID(appNumber) != CFBooleanGetTypeID(),
          let appID = account["app_id"] as? Int, appID > 0 else { return nil }
    return try? JSONSerialization.data(withJSONObject: c)
}
