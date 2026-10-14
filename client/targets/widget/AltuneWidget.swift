import AppIntents
import SwiftUI
import UIKit
import WidgetKit

private let widgetKind = "AltuneWidget"
private let snapshotDefaultsKey = "widgetSnapshot"

private enum Palette {
  static let canvas = Color(red: 0x12 / 255, green: 0x12 / 255, blue: 0x14 / 255)
  static let neutralTop = Color(red: 0x1E / 255, green: 0x1E / 255, blue: 0x23 / 255)
  static let neutralBottom = Color(red: 0x15 / 255, green: 0x15 / 255, blue: 0x18 / 255)
  static let textPrimary = Color(red: 0xF4 / 255, green: 0xF4 / 255, blue: 0xF6 / 255)
  static let textSecondary = Color(red: 0xA6 / 255, green: 0xA6 / 255, blue: 0xAE / 255)
  static let emptyGlyph = Color(red: 0x74 / 255, green: 0x74 / 255, blue: 0x7E / 255)
  static let pillInk = Color(red: 0x18 / 255, green: 0x18 / 255, blue: 0x1C / 255)
  static let activeToggle = Color(red: 0x6B / 255, green: 0x8F / 255, blue: 0xFF / 255)
  static let scrimInk = Color(red: 6 / 255, green: 5 / 255, blue: 17 / 255)
}

private enum TypeScale {
  static let small: CGFloat = 11
  static let medium: CGFloat = 14.67
  static let large: CGFloat = 19.55
  static let progressTimes: CGFloat = 10
}

enum RepeatMode: String, Decodable {
  case off, all, one
}

struct NowPlaying: Decodable {
  let title: String
  let artist: String
  let source: String?
  let isPlaying: Bool
  let shuffle: Bool
  let `repeat`: RepeatMode
  let positionSec: Double
  let durationSec: Double
  let coverFileName: String?
}

private struct StoredSnapshot: Decodable {
  let signedIn: Bool
  let nowPlaying: NowPlaying?
}

enum WidgetState {
  case signedOut
  case nothingPlayed
  case playing(NowPlaying, cover: UIImage?)
}

struct NowPlayingEntry: TimelineEntry {
  let date: Date
  let state: WidgetState
}

private func currentWidgetState() -> WidgetState {
  guard
    let snapshotJson = UserDefaults(suiteName: resolvedAppGroup())?.string(forKey: snapshotDefaultsKey),
    let snapshot = try? JSONDecoder().decode(StoredSnapshot.self, from: Data(snapshotJson.utf8))
  else {
    return .nothingPlayed
  }
  guard snapshot.signedIn else {
    return .signedOut
  }
  guard let nowPlaying = snapshot.nowPlaying else {
    return .nothingPlayed
  }
  return .playing(nowPlaying, cover: storedCover(named: nowPlaying.coverFileName))
}

private func storedCover(named coverFileName: String?) -> UIImage? {
  guard
    let coverFileName,
    let container = FileManager.default.containerURL(
      forSecurityApplicationGroupIdentifier: resolvedAppGroup()
    )
  else {
    return nil
  }
  return UIImage(contentsOfFile: container.appendingPathComponent(coverFileName).path)
}

struct NowPlayingProvider: TimelineProvider {
  func placeholder(in context: Context) -> NowPlayingEntry {
    NowPlayingEntry(date: .now, state: .nothingPlayed)
  }

  func getSnapshot(in context: Context, completion: @escaping (NowPlayingEntry) -> Void) {
    completion(NowPlayingEntry(date: .now, state: currentWidgetState()))
  }

  func getTimeline(in context: Context, completion: @escaping (Timeline<NowPlayingEntry>) -> Void) {
    let entry = NowPlayingEntry(date: .now, state: currentWidgetState())
    completion(Timeline(entries: [entry], policy: .never))
  }
}

struct AltuneWidgetView: View {
  @Environment(\.widgetFamily) private var family
  let entry: NowPlayingEntry

  var body: some View {
    content
      .containerBackground(for: .widget) { Palette.canvas }
  }

  @ViewBuilder
  private var content: some View {
    switch (entry.state, family) {
    case (.signedOut, .systemMedium):
      MediumEmptyView(message: .signedOut)
    case (.signedOut, _):
      SmallEmptyView(message: .signedOut)
    case (.nothingPlayed, .systemMedium):
      MediumEmptyView(message: .nothingPlayed)
    case (.nothingPlayed, _):
      SmallEmptyView(message: .nothingPlayed)
    case let (.playing(nowPlaying, cover), .systemMedium):
      MediumNowPlayingView(nowPlaying: nowPlaying, cover: cover)
    case let (.playing(nowPlaying, cover), _):
      SmallNowPlayingView(nowPlaying: nowPlaying, cover: cover)
    }
  }
}

struct SmallNowPlayingView: View {
  let nowPlaying: NowPlaying
  let cover: UIImage?

  var body: some View {
    ZStack(alignment: .bottomLeading) {
      CoverFill(cover: cover)
      LinearGradient(
        stops: [
          .init(color: .clear, location: 0.24),
          .init(color: Palette.scrimInk.opacity(0.52), location: 0.5),
          .init(color: Palette.scrimInk.opacity(0.95), location: 1),
        ],
        startPoint: .top,
        endPoint: .bottom
      )
      VStack(alignment: .leading, spacing: 0) {
        VStack(alignment: .leading, spacing: 2) {
          Text(nowPlaying.title)
            .font(.system(size: TypeScale.medium, weight: .bold))
            .tracking(-0.18)
            .foregroundStyle(Palette.textPrimary)
            .lineLimit(2)
          Text(nowPlaying.artist)
            .font(.system(size: TypeScale.small, weight: .medium))
            .foregroundStyle(.white.opacity(0.82))
            .lineLimit(1)
        }
        .shadow(color: .black.opacity(0.5), radius: 3, y: 1)
        .padding(.horizontal, 16)
        HStack(spacing: 0) {
          TransportButton(intent: PreviousIntent(), systemImage: "backward.fill", label: "Previous track")
          Spacer(minLength: 0)
          PlayPauseDisc(isPlaying: nowPlaying.isPlaying)
          Spacer(minLength: 0)
          TransportButton(intent: NextIntent(), systemImage: "forward.fill", label: "Next track")
        }
        .frame(height: 52)
        .padding(.horizontal, 9)
        .padding(.bottom, 9)
      }
    }
  }
}

struct MediumNowPlayingView: View {
  let nowPlaying: NowPlaying
  let cover: UIImage?

  var body: some View {
    ZStack(alignment: .topLeading) {
      CoverFill(cover: cover)
      MediumScrim()
      TrackIdentity(nowPlaying: nowPlaying)
        .padding(.leading, 18)
        .padding(.top, 18)
        .padding(.trailing, 150)
      StaticProgress(positionSec: nowPlaying.positionSec, durationSec: nowPlaying.durationSec)
        .padding(.horizontal, 18)
        .padding(.top, 84)
      FullControlRow(nowPlaying: nowPlaying)
        .padding(.horizontal, 16)
        .padding(.bottom, 12)
        .frame(maxHeight: .infinity, alignment: .bottom)
    }
  }
}

struct TrackIdentity: View {
  let nowPlaying: NowPlaying

  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      Text(nowPlaying.title)
        .font(.system(size: TypeScale.large, weight: .bold))
        .tracking(-0.23)
        .foregroundStyle(Palette.textPrimary)
        .lineLimit(1)
        .shadow(color: .black.opacity(0.5), radius: 4, y: 1)
      Text(nowPlaying.artist)
        .font(.system(size: TypeScale.small, weight: .medium))
        .foregroundStyle(.white.opacity(0.85))
        .lineLimit(1)
        .padding(.top, 2)
      if let source = nowPlaying.source {
        Text(source)
          .font(.system(size: TypeScale.small, weight: .medium))
          .foregroundStyle(.white.opacity(0.62))
          .lineLimit(1)
          .padding(.top, 3)
      }
    }
  }
}

struct MediumScrim: View {
  var body: some View {
    ZStack {
      LinearGradient(
        stops: [
          .init(color: Palette.scrimInk.opacity(0.93), location: 0),
          .init(color: Palette.scrimInk.opacity(0.5), location: 0.34),
          .init(color: Palette.scrimInk.opacity(0), location: 0.6),
        ],
        startPoint: .leading,
        endPoint: .trailing
      )
      LinearGradient(
        stops: [
          .init(color: Palette.scrimInk.opacity(0.9), location: 0),
          .init(color: Palette.scrimInk.opacity(0.3), location: 0.44),
          .init(color: .clear, location: 0.6),
        ],
        startPoint: .bottom,
        endPoint: .top
      )
    }
  }
}

struct StaticProgress: View {
  let positionSec: Double
  let durationSec: Double

  private var elapsedFraction: Double {
    guard durationSec > 0 else { return 0 }
    return min(max(positionSec / durationSec, 0), 1)
  }

  var body: some View {
    VStack(spacing: 6) {
      GeometryReader { track in
        ZStack(alignment: .leading) {
          Capsule().fill(Palette.textPrimary.opacity(0.2))
          Capsule()
            .fill(Palette.activeToggle)
            .frame(width: track.size.width * elapsedFraction)
        }
      }
      .frame(height: 4)
      HStack {
        Text(playbackClock(positionSec))
        Spacer(minLength: 0)
        Text(playbackClock(durationSec))
      }
      .font(.system(size: TypeScale.progressTimes, weight: .medium).monospacedDigit())
      .foregroundStyle(.white.opacity(0.6))
    }
  }
}

private func playbackClock(_ seconds: Double) -> String {
  Duration.seconds(max(seconds, 0)).formatted(.time(pattern: .minuteSecond))
}

struct FullControlRow: View {
  let nowPlaying: NowPlaying

  var body: some View {
    HStack(spacing: 0) {
      ToggleButton(
        intent: ShuffleIntent(),
        systemImage: "shuffle",
        isActive: nowPlaying.shuffle,
        label: nowPlaying.shuffle ? "Shuffle, on" : "Shuffle, off"
      )
      Spacer(minLength: 0)
      TransportButton(intent: PreviousIntent(), systemImage: "backward.fill", label: "Previous track")
      Spacer(minLength: 0)
      PlayPauseDisc(isPlaying: nowPlaying.isPlaying)
      Spacer(minLength: 0)
      TransportButton(intent: NextIntent(), systemImage: "forward.fill", label: "Next track")
      Spacer(minLength: 0)
      RepeatButton(mode: nowPlaying.`repeat`)
    }
    .frame(height: 52)
  }
}

struct TransportButton<Intent: AppIntent>: View {
  let intent: Intent
  let systemImage: String
  let label: String

  var body: some View {
    Button(intent: intent) {
      Image(systemName: systemImage)
        .font(.system(size: 20, weight: .semibold))
        .foregroundStyle(Palette.textPrimary)
        .frame(width: 48, height: 52)
        .contentShape(Rectangle())
    }
    .buttonStyle(.plain)
    .accessibilityLabel(label)
  }
}

struct ToggleButton<Intent: AppIntent>: View {
  let intent: Intent
  let systemImage: String
  let isActive: Bool
  let label: String

  var body: some View {
    Button(intent: intent) {
      Image(systemName: systemImage)
        .font(.system(size: 18, weight: .semibold))
        .foregroundStyle(isActive ? Palette.activeToggle : Palette.textPrimary.opacity(0.5))
        .frame(width: 44, height: 52)
        .contentShape(Rectangle())
    }
    .buttonStyle(.plain)
    .accessibilityLabel(label)
  }
}

struct RepeatButton: View {
  let mode: RepeatMode

  var body: some View {
    ToggleButton(
      intent: RepeatIntent(),
      systemImage: mode == .one ? "repeat.1" : "repeat",
      isActive: mode != .off,
      label: spokenLabel
    )
  }

  private var spokenLabel: String {
    switch mode {
    case .off: "Repeat, off"
    case .all: "Repeat all, on"
    case .one: "Repeat one, on"
    }
  }
}

struct PlayPauseDisc: View {
  let isPlaying: Bool

  var body: some View {
    Button(intent: PlayPauseIntent()) {
      Image(systemName: isPlaying ? "pause.fill" : "play.fill")
        .font(.system(size: 20, weight: .bold))
        .foregroundStyle(Palette.canvas)
        .frame(width: 52, height: 52)
        .background(Circle().fill(Palette.textPrimary))
    }
    .buttonStyle(.plain)
    .accessibilityLabel(isPlaying ? "Pause" : "Play")
  }
}

struct CoverFill: View {
  let cover: UIImage?

  var body: some View {
    if let cover {
      GeometryReader { widget in
        Image(uiImage: cover)
          .resizable()
          .scaledToFill()
          .frame(width: widget.size.width, height: widget.size.height)
          .clipped()
      }
    } else {
      NeutralBackground()
    }
  }
}

struct NeutralBackground: View {
  var body: some View {
    LinearGradient(
      colors: [Palette.neutralTop, Palette.neutralBottom],
      startPoint: .top,
      endPoint: .bottom
    )
  }
}

struct EmptyStateMessage {
  let headline: String
  let detail: String
  let pillTitle: String
  let pillSymbol: String?

  static let nothingPlayed = EmptyStateMessage(
    headline: "Open Altune to start a playlist",
    detail: "Nothing playing",
    pillTitle: "Open",
    pillSymbol: "arrow.up.right"
  )

  static let signedOut = EmptyStateMessage(
    headline: "Sign in to Altune",
    detail: "Your playlists will show up here.",
    pillTitle: "Sign in",
    pillSymbol: nil
  )
}

struct SmallEmptyView: View {
  let message: EmptyStateMessage

  var body: some View {
    ZStack(alignment: .topLeading) {
      NeutralBackground()
      VStack(alignment: .leading, spacing: 0) {
        EmptyStateGlyph()
        Text(message.detail)
          .font(.system(size: TypeScale.small, weight: .medium))
          .foregroundStyle(Palette.textSecondary)
          .lineLimit(1)
          .padding(.top, 4)
        Text(message.headline)
          .font(.system(size: TypeScale.medium, weight: .bold))
          .tracking(-0.18)
          .foregroundStyle(Palette.textPrimary)
          .lineLimit(2)
          .padding(.top, 3)
        Spacer(minLength: 0)
        EmptyStatePill(message: message)
      }
      .padding(16)
    }
  }
}

struct MediumEmptyView: View {
  let message: EmptyStateMessage

  var body: some View {
    ZStack(alignment: .topLeading) {
      NeutralBackground()
      EmptyStateGlyph()
        .padding(16)
      VStack(alignment: .leading, spacing: 0) {
        Text(message.headline)
          .font(.system(size: TypeScale.large, weight: .bold))
          .tracking(-0.23)
          .foregroundStyle(Palette.textPrimary)
          .lineLimit(2)
          .minimumScaleFactor(0.8)
        Text(message.detail)
          .font(.system(size: TypeScale.small, weight: .medium))
          .foregroundStyle(Palette.textSecondary)
          .lineLimit(1)
          .padding(.top, 4)
        EmptyStatePill(message: message)
          .padding(.top, 14)
      }
      .frame(maxHeight: .infinity)
      .padding(.leading, 178)
      .padding(.trailing, 16)
    }
  }
}

struct EmptyStateGlyph: View {
  var body: some View {
    Image(systemName: "music.note.list")
      .font(.system(size: 24, weight: .regular))
      .foregroundStyle(Palette.emptyGlyph)
      .frame(width: 30, height: 30)
  }
}

struct EmptyStatePill: View {
  let message: EmptyStateMessage

  var body: some View {
    HStack(spacing: 6) {
      Text(message.pillTitle)
        .font(.system(size: TypeScale.medium, weight: .bold))
        .tracking(-0.15)
      if let pillSymbol = message.pillSymbol {
        Image(systemName: pillSymbol)
          .font(.system(size: 12, weight: .bold))
      }
    }
    .foregroundStyle(Palette.pillInk)
    .padding(.leading, 20)
    .padding(.trailing, 18)
    .frame(height: 44)
    .background(Capsule().fill(Palette.textPrimary))
  }
}

@main
struct AltuneWidget: Widget {
  var body: some WidgetConfiguration {
    StaticConfiguration(kind: widgetKind, provider: NowPlayingProvider()) { entry in
      AltuneWidgetView(entry: entry)
    }
    .configurationDisplayName("Altune")
    .description("What's playing, with playback controls.")
    .supportedFamilies([.systemSmall, .systemMedium])
    .contentMarginsDisabled()
  }
}
