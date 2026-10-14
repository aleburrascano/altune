import Foundation

private let hardcodedAppGroup = "group.com.altune.app"
private let appGroupPrefix = "group.com.altune.app"
private let applicationGroupsEntitlement = "com.apple.security.application-groups"
private let altAppGroupsInfoKey = "ALTAppGroups"

func resolvedAppGroup() -> String {
  appGroupFromProvisioningProfile()
    ?? appGroupFromInfoPlist()
    ?? hardcodedAppGroup
}

private func appGroupFromProvisioningProfile() -> String? {
  guard
    let profileURL = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision"),
    let profileData = try? Data(contentsOf: profileURL),
    let entitlements = provisioningEntitlements(from: profileData),
    let groups = entitlements[applicationGroupsEntitlement] as? [String]
  else {
    return nil
  }
  return groups.first { $0.hasPrefix(appGroupPrefix) }
}

private func appGroupFromInfoPlist() -> String? {
  guard let groups = Bundle.main.object(forInfoDictionaryKey: altAppGroupsInfoKey) as? [String] else {
    return nil
  }
  return groups.first { $0.hasPrefix(appGroupPrefix) }
}

private func provisioningEntitlements(from profileData: Data) -> [String: Any]? {
  guard
    let plistData = embeddedPlist(in: profileData),
    let object = try? PropertyListSerialization.propertyList(from: plistData, options: [], format: nil),
    let plist = object as? [String: Any]
  else {
    return nil
  }
  return plist["Entitlements"] as? [String: Any]
}

private func embeddedPlist(in profileData: Data) -> Data? {
  guard
    let open = profileData.range(of: Data("<plist".utf8)),
    let close = profileData.range(
      of: Data("</plist>".utf8),
      in: open.lowerBound..<profileData.endIndex
    )
  else {
    return nil
  }
  return profileData.subdata(in: open.lowerBound..<close.upperBound)
}
