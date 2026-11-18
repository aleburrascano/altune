export function extFromUrl(url: string): string {
  const query = url.indexOf('?');
  const path = query === -1 ? url : url.slice(0, query);
  const slash = path.lastIndexOf('/');
  const dot = path.lastIndexOf('.');
  return dot > slash && dot < path.length - 1 ? path.slice(dot) : '.mp3';
}
