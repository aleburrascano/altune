export function resolveAfter<T>(ms: number, value: T): Promise<T> {
  return new Promise((resolve) => {
    setTimeout(() => {
      resolve(value);
    }, ms);
  });
}

export function rejectAfter(ms: number, error: Error): Promise<never> {
  return new Promise((_, reject) => {
    setTimeout(() => {
      reject(error);
    }, ms);
  });
}
