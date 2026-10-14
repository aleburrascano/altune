@MainActor
enum IntentBridge {
  private static var bufferedCommands = NewestCommands()

  static var handler: ((String) -> Void)? {
    didSet { deliverBufferedCommands() }
  }

  static func send(_ command: String) {
    guard let handler else {
      bufferedCommands.append(command)
      return
    }
    handler(command)
  }

  private static func deliverBufferedCommands() {
    guard let handler else { return }
    bufferedCommands.takeAll().forEach(handler)
  }
}

struct NewestCommands {
  private static let capacity = 32
  private var commands: [String] = []

  mutating func append(_ command: String) {
    commands.append(command)
    let overflow = commands.count - Self.capacity
    if overflow > 0 {
      commands.removeFirst(overflow)
    }
  }

  mutating func takeAll() -> [String] {
    let taken = commands
    commands = []
    return taken
  }
}
