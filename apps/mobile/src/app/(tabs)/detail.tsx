/**
 * /detail route — renders the result detail screen.
 *
 * Inside (tabs) so the tab bar remains visible. The href: null option in
 * _layout.tsx hides it from the tab bar UI while keeping the tab bar visible
 * when viewing this route. The screen reads the in-memory handoff; a cold
 * start with no handoff redirects to /discover.
 */

import { DetailScreen } from '../../features/detail/ui/DetailScreen';

export default DetailScreen;
