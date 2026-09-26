export type IsAssignable<From, To> = [From] extends [To] ? true : false;

export type IsExactlyAssignable<From, To> = true extends (
  To extends unknown
    ? [From] extends [To]
      ? [Exclude<keyof From, keyof To>] extends [never]
        ? true
        : false
      : false
    : never
)
  ? true
  : false;

export type Not<T extends boolean> = T extends true ? false : true;

export function expectType<T extends true>(_witness?: T): void {}
