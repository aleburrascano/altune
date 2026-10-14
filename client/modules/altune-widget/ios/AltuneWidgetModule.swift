public import ExpoModulesCore
import UIKit
import WidgetKit

private let widgetCommandEvent = "onWidgetCommand"
private let snapshotDefaultsKey = "widgetSnapshot"
private let fileNamesThatEscapeTheContainer: Set<String> = ["", ".", ".."]
private let coverDownloadTimeoutSeconds: TimeInterval = 10
private let coverLongestSidePoints: CGFloat = 300
private let coverJpegQuality: CGFloat = 0.8
private let successfulHttpStatusCodes = 200..<300

public class AltuneWidgetModule: Module {
  private lazy var commandRelay = WidgetCommandRelay { [weak self] command in
    self?.sendEvent(widgetCommandEvent, ["command": command])
  }

  public func definition() -> ModuleDefinition {
    Name("AltuneWidget")

    Events(widgetCommandEvent)

    OnCreate {
      let relay = commandRelay
      onMainActor { relay.attachToIntentBridge() }
    }

    OnDestroy {
      let relay = commandRelay
      onMainActor { relay.detachFromIntentBridge() }
    }

    OnStartObserving(widgetCommandEvent) {
      let relay = commandRelay
      onMainActor { relay.startDelivering() }
    }

    OnStopObserving(widgetCommandEvent) {
      let relay = commandRelay
      onMainActor { relay.holdDeliveries() }
    }

    Function("drainPendingCommands") {
      let relay = commandRelay
      onMainActor { relay.deliverHeldCommands() }
    }

    Function("writeSnapshot") { (json: String) in
      try appGroupDefaults().set(json, forKey: snapshotDefaultsKey)
    }

    AsyncFunction("writeCoverFromUrl") { (url: URL, fileName: String) in
      try await writeCoverIntoAppGroup(from: url, fileName: fileName)
    }

    Function("reloadWidget") {
      WidgetCenter.shared.reloadAllTimelines()
    }
  }
}

@MainActor
private final class WidgetCommandRelay {
  private static weak var relayAttachedToIntentBridge: WidgetCommandRelay?

  private let emit: (String) -> Void
  private var heldCommands = NewestCommands()
  private var isJavaScriptListening = false

  nonisolated init(emit: @escaping (String) -> Void) {
    self.emit = emit
  }

  func attachToIntentBridge() {
    Self.relayAttachedToIntentBridge = self
    IntentBridge.handler = { [weak self] command in
      self?.receive(command)
    }
  }

  func detachFromIntentBridge() {
    guard Self.relayAttachedToIntentBridge === self else { return }
    IntentBridge.handler = nil
  }

  func startDelivering() {
    isJavaScriptListening = true
    deliverHeldCommands()
  }

  func holdDeliveries() {
    isJavaScriptListening = false
  }

  func deliverHeldCommands() {
    guard isJavaScriptListening else { return }
    heldCommands.takeAll().forEach(emit)
  }

  private func receive(_ command: String) {
    guard isJavaScriptListening else {
      heldCommands.append(command)
      return
    }
    emit(command)
  }
}

private func onMainActor(_ work: @escaping @Sendable @MainActor () -> Void) {
  DispatchQueue.main.async {
    MainActor.assumeIsolated(work)
  }
}

private func appGroupDefaults() throws -> UserDefaults {
  guard let defaults = UserDefaults(suiteName: resolvedAppGroup()) else {
    throw AppGroupUnavailableException()
  }
  return defaults
}

private func writeCoverIntoAppGroup(from url: URL, fileName: String) async throws {
  guard isPlainFileName(fileName) else {
    throw CoverFileNameEscapesContainerException(fileName)
  }
  let coverFile = try appGroupContainer().appendingPathComponent(fileName)
  let downloadedCover = try await downloadCover(from: url)
  guard let cover = UIImage(data: downloadedCover) else {
    throw CoverIsNotAnImageException(url.absoluteString)
  }
  guard let coverJpeg = downscaledToWidgetSize(cover).jpegData(compressionQuality: coverJpegQuality) else {
    throw CoverIsNotAnImageException(url.absoluteString)
  }
  try coverJpeg.write(to: coverFile, options: .atomic)
}

private func appGroupContainer() throws -> URL {
  guard
    let container = FileManager.default.containerURL(
      forSecurityApplicationGroupIdentifier: resolvedAppGroup()
    )
  else {
    throw AppGroupUnavailableException()
  }
  return container
}

private func downloadCover(from url: URL) async throws -> Data {
  var request = URLRequest(url: url)
  request.timeoutInterval = coverDownloadTimeoutSeconds
  let (body, response) = try await URLSession.shared.data(for: request)
  guard let httpResponse = response as? HTTPURLResponse else {
    return body
  }
  guard successfulHttpStatusCodes.contains(httpResponse.statusCode) else {
    throw CoverDownloadFailedException(httpResponse.statusCode)
  }
  return body
}

private func downscaledToWidgetSize(_ cover: UIImage) -> UIImage {
  let longestSide = max(cover.size.width, cover.size.height)
  guard longestSide > coverLongestSidePoints else {
    return cover
  }
  let scale = coverLongestSidePoints / longestSide
  let targetSize = CGSize(width: cover.size.width * scale, height: cover.size.height * scale)
  let format = UIGraphicsImageRendererFormat()
  format.scale = 1
  return UIGraphicsImageRenderer(size: targetSize, format: format).image { _ in
    cover.draw(in: CGRect(origin: .zero, size: targetSize))
  }
}

private func isPlainFileName(_ fileName: String) -> Bool {
  !fileNamesThatEscapeTheContainer.contains(fileName) && !fileName.contains("/")
}

internal final class AppGroupUnavailableException: Exception, @unchecked Sendable {
  override var reason: String {
    "The App Group container is not available to this build"
  }
}

internal final class CoverIsNotAnImageException: GenericException<String>, @unchecked Sendable {
  override var reason: String {
    "The widget cover at '\(param)' is not an image"
  }
}

internal final class CoverDownloadFailedException: GenericException<Int>, @unchecked Sendable {
  override var reason: String {
    "The widget cover download failed with HTTP status \(param)"
  }
}

internal final class CoverFileNameEscapesContainerException: GenericException<String>, @unchecked Sendable {
  override var reason: String {
    "The widget cover file name '\(param)' is not a plain file name"
  }
}
