// Compatibility barrel. The primitive decoders live in wireDecoders.ts and each
// response parser lives beside its domain's endpoints; this file only re-exports
// the names imported from outside the owning module so existing importers stay
// stable.
export {
  asArray,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  member,
  nullableNumber,
  nullableString,
  parseErrorBody,
} from './wireDecoders';
export { parseListTracksResponse, parseTrackResponse, tryParseTrackResponse } from './tracks';
export { parseListAlbumsResponse, parseListArtistsResponse } from './library';
export { parseDiscoverySearchResponse } from './discovery';
