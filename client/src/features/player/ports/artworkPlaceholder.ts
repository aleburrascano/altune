type ResolvePlaceholderUri = () => string;

function noPlaceholderInstalled(): string {
  return '';
}

let resolveUri: ResolvePlaceholderUri = noPlaceholderInstalled;

export function installArtworkPlaceholder(resolve: ResolvePlaceholderUri): void {
  resolveUri = resolve;
}

export function artworkPlaceholderUri(): string {
  return resolveUri();
}
