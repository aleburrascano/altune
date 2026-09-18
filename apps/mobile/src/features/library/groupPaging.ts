/** The server's own default page for these lists, so a page means the same on both sides. */
export const GROUP_PAGE_SIZE = 50;

/**
 * Where the next page of an album, artist or playlist grid starts, or undefined at the end of
 * the list.
 *
 * None of those lists carries has_more, and the total each returns counts its own page rather
 * than the library, so a page shorter than the one asked for is the only end-of-list signal.
 * That also ends the scroll on an empty page, which would otherwise re-request its own offset
 * forever (#1697).
 */
export function nextGroupPageOffset(pageLength: number, pageOffset: number): number | undefined {
  return pageLength < GROUP_PAGE_SIZE ? undefined : pageOffset + pageLength;
}
