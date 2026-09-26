import type { DetailFact } from './DetailFacts';

export type ArtistFactsInput = {
  releases: number;
  inLibrary: number;
  listeners: string | null;
};

function releasesFact(releases: number): DetailFact | null {
  return releases > 0 ? { label: 'Releases', value: String(releases) } : null;
}

function inLibraryFact(inLibrary: number): DetailFact | null {
  return inLibrary > 0 ? { label: 'In library', value: String(inLibrary) } : null;
}

function listenersFact(listeners: string | null): DetailFact | null {
  return listeners !== null ? { label: 'Listeners', value: listeners } : null;
}

export function buildArtistFacts(input: ArtistFactsInput): (DetailFact | null)[] {
  return [releasesFact(input.releases), inLibraryFact(input.inLibrary), listenersFact(input.listeners)];
}
