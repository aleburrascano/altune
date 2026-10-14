import { NativeModule, requireOptionalNativeModule } from 'expo';

export type WidgetCommandEvent = {
  command: string;
};

export type WidgetCommandListener = (event: WidgetCommandEvent) => void;

export type WidgetCommandSubscription = {
  remove(): void;
};

type AltuneWidgetEvents = {
  onWidgetCommand: WidgetCommandListener;
};

declare class AltuneWidgetNativeModule extends NativeModule<AltuneWidgetEvents> {
  writeSnapshot(json: string): void;
  writeCoverFromUrl(url: string, fileName: string): Promise<void>;
  reloadWidget(): void;
  drainPendingCommands(): void;
}

const altuneWidgetOnIos = requireOptionalNativeModule<AltuneWidgetNativeModule>('AltuneWidget');

const subscriptionWherePlatformHasNoWidget: WidgetCommandSubscription = {
  remove: () => undefined,
};

export function addWidgetCommandListener(listener: WidgetCommandListener): WidgetCommandSubscription {
  if (!altuneWidgetOnIos) return subscriptionWherePlatformHasNoWidget;
  return altuneWidgetOnIos.addListener('onWidgetCommand', listener);
}

export function drainPendingCommands(): void {
  altuneWidgetOnIos?.drainPendingCommands();
}

export function writeSnapshot(json: string): void {
  altuneWidgetOnIos?.writeSnapshot(json);
}

export async function writeCoverFromUrl(url: string, fileName: string): Promise<void> {
  await altuneWidgetOnIos?.writeCoverFromUrl(url, fileName);
}

export function reloadWidget(): void {
  altuneWidgetOnIos?.reloadWidget();
}
