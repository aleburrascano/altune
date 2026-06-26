// Top-level (NOT under the (auth) group) so AuthGate lets the recovery
// session through to it — see AuthGate's onRecoveryRoute allowance.
import { SetNewPasswordScreen } from '../features/auth/ui/SetNewPasswordScreen';

export default SetNewPasswordScreen;
