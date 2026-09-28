export const GROUP_PAGE_SIZE = 50;

export function nextGroupPageOffset(
  pageLength: number,
  pageOffset: number,
  pageSize: number = GROUP_PAGE_SIZE
): number | undefined {
  return pageLength < pageSize ? undefined : pageOffset + pageLength;
}
