import AppIntents
import SwiftUI
import WidgetKit

private let tapsKey = "carplaySpikeTaps"

struct TapIntent: AppIntent {
  static let title: LocalizedStringResource = "Tap"

  func perform() async throws -> some IntentResult {
    let defaults = UserDefaults.standard
    defaults.set(defaults.integer(forKey: tapsKey) + 1, forKey: tapsKey)
    return .result()
  }
}

struct TapEntry: TimelineEntry {
  let date: Date
  let taps: Int
}

struct TapProvider: TimelineProvider {
  func placeholder(in context: Context) -> TapEntry {
    TapEntry(date: .now, taps: 0)
  }

  func getSnapshot(in context: Context, completion: @escaping (TapEntry) -> Void) {
    completion(currentEntry())
  }

  func getTimeline(in context: Context, completion: @escaping (Timeline<TapEntry>) -> Void) {
    completion(Timeline(entries: [currentEntry()], policy: .never))
  }

  private func currentEntry() -> TapEntry {
    TapEntry(date: .now, taps: UserDefaults.standard.integer(forKey: tapsKey))
  }
}

struct AltuneCarWidgetView: View {
  let entry: TapEntry

  var body: some View {
    VStack(spacing: 8) {
      Text("Altune").font(.headline)
      Button(intent: TapIntent()) {
        Label("\(entry.taps)", systemImage: "hand.tap")
      }
    }
    .containerBackground(.black, for: .widget)
  }
}

@main
struct AltuneCarWidget: Widget {
  var body: some WidgetConfiguration {
    StaticConfiguration(kind: "AltuneCarWidget", provider: TapProvider()) { entry in
      AltuneCarWidgetView(entry: entry)
    }
    .configurationDisplayName("Altune")
    .description("CarPlay widget test")
    .supportedFamilies([.systemSmall])
  }
}
