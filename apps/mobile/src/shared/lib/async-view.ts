export type AsyncView = 'loading' | 'error' | 'empty' | 'ready';

export type AsyncInputs = {
  isLoading: boolean;
  isError: boolean;
  isEmpty: boolean;
};

export function asyncView({ isLoading, isError, isEmpty }: AsyncInputs): AsyncView {
  if (isLoading) {
    return 'loading';
  }
  if (isError) {
    return 'error';
  }
  if (isEmpty) {
    return 'empty';
  }
  return 'ready';
}
