// Pressing "previous" past this many ms into the current track restarts it
// (seek to 0); pressing while under it steps back to the previous track. Shared
// by the in-app player button (FullPlayer) and the lock-screen/remote control
// handler (playback service) so the two entry points cannot drift apart.
export const RESTART_THRESHOLD_MS = 3_000;
