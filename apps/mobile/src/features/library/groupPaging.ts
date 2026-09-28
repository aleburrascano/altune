export const GROUP_PAGE_SIZE = 50;

export function nextGroupPageOffset(pageLength: number, pageOffset: number): number | undefined {
  return pageLength < GROUP_PAGE_SIZE ? undefined : pageOffset + pageLength;
}
