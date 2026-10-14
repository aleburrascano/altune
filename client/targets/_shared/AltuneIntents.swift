import AppIntents

struct PlayPauseIntent: AudioPlaybackIntent {
  static var title: LocalizedStringResource { "Play or Pause" }

  init() {}

  func perform() async throws -> some IntentResult {
    await IntentBridge.send("play-pause")
    return .result()
  }
}

struct NextIntent: AudioPlaybackIntent {
  static var title: LocalizedStringResource { "Next Track" }

  init() {}

  func perform() async throws -> some IntentResult {
    await IntentBridge.send("next")
    return .result()
  }
}

struct PreviousIntent: AudioPlaybackIntent {
  static var title: LocalizedStringResource { "Previous Track" }

  init() {}

  func perform() async throws -> some IntentResult {
    await IntentBridge.send("previous")
    return .result()
  }
}

struct ShuffleIntent: AudioPlaybackIntent {
  static var title: LocalizedStringResource { "Toggle Shuffle" }

  init() {}

  func perform() async throws -> some IntentResult {
    await IntentBridge.send("shuffle")
    return .result()
  }
}

struct RepeatIntent: AudioPlaybackIntent {
  static var title: LocalizedStringResource { "Cycle Repeat" }

  init() {}

  func perform() async throws -> some IntentResult {
    await IntentBridge.send("repeat")
    return .result()
  }
}
