type TabRoute = { key: string; name: string };
type TabDescriptor = { options: { title?: string | undefined } };
type TabPressEvent = { type: 'tabPress'; target: string; canPreventDefault: true };
type TabPressResult = { defaultPrevented: boolean };

export interface TabBarProps {
  state: { index: number; routes: readonly TabRoute[] };
  descriptors: Record<string, TabDescriptor | undefined>;
  navigation: {
    emit: (event: TabPressEvent) => TabPressResult;
    navigate: (name: string) => void;
  };
}
