/**
 * /detail route — renders the result detail screen.
 *
 * Deliberately a SIBLING of (tabs), so it mounts in the root _layout <Slot/>
 * (inside AuthGate) rather than inside the tab group — the tab bar is hidden
 * on this screen. The screen reads the in-memory handoff; a cold start with no
 * handoff redirects to /discover.
 */

import { DetailScreen } from '../features/detail/ui/DetailScreen';

export default DetailScreen;
