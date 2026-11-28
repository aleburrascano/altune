`signIn` owns how a person signs in, signs up, resets a password and returns from an email link.

The door, `index.ts`, exports only what `mobile` and the routes use: the five screens (`AuthCallbackScreen`, `ForgotPasswordScreen`, `SetNewPasswordScreen`, `SignInScreen`, `SignUpScreen`), `AuthGate`, `SignInProvider`, the four factories (`createAttemptLockoutStore`, `createRecoveryUnlockStore`, `createSignInHealth`, `createAuthIntents`), `startAppAuthDeepLinks`, and the adapters `httpAuthPort`, `nativeAuthBrowser` and `nativeLinkEvents`. Tests reach the parts directly.

- `core/`: the rules and types (`authIntent`, `authRedirect`, `parseAuthLink`, `validation`, `signInHealth` and more), and the client state as factories: `createAttemptLockoutStore`, `createRecoveryUnlockStore` and `createConsumedCredentialStore`. `mobile` builds each once and passes it through `SignInProvider`. `errors.ts` holds the failure reasons and their classification.
- `ports/ports.ts`: `AuthPort`, `AuthBrowser`, `AttemptLockout`, `AuthIntents` and `LinkEvents`, which `mobile` fills.
- `shell/`: the non-React use cases. `createAuthIntents` completes an email or OAuth link and spends each credential once. `authDeepLinks` starts listening for links.
- `adapters/http/`: `signInHttp.ts` fills `AuthPort` and posts failure reports, and `mapping.ts` builds the wire body. `adapters/native/` holds the browser and link-event adapters.
- `view/`: `SignInProvider.tsx`, the screens and the `use<Thing>.ts` hooks.
- `__tests__/`: every test, with the module's own helpers in `authTestUtils.ts` and `fakes.ts`.
