export interface UpdatesState {
  readonly isEnabled: boolean;
  readonly isEmbeddedLaunch: boolean;
  readonly updateId: string | null;
}

export interface AppRuntime {
  manifestVersion(): string | undefined;
  declaredScheme(): string | string[] | undefined;
  executionEnvironment(): string;
  updates(): UpdatesState;
}

export interface Build {
  readonly platform: string;
  readonly commit: string | undefined;
}
