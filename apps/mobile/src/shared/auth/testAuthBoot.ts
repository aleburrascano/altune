let settle: () => void = () => {};
const settled = new Promise<void>((resolve) => {
  settle = resolve;
});

export function isTestAuthEnabled(): boolean {
  return __DEV__ === true && process.env.EXPO_PUBLIC_TEST_AUTH === '1';
}

export function markTestAuthBootSettled(): void {
  settle();
}

export function testAuthBootSettled(): Promise<void> {
  return isTestAuthEnabled() ? settled : Promise.resolve();
}
