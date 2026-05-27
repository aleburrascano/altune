import { useState } from 'react';
import { Pressable, Text, TextInput, View } from 'react-native';

import { useSignUp } from '../hooks/useSignUp';

const styles = {
  container: {
    flex: 1,
    padding: 24,
    justifyContent: 'center' as const,
    gap: 12,
  },
  input: { borderWidth: 1, borderColor: '#888', padding: 12, borderRadius: 6 },
  button: {
    backgroundColor: '#222',
    padding: 14,
    borderRadius: 6,
    alignItems: 'center' as const,
  },
  buttonText: { color: '#fff' },
  error: { color: '#c00' },
};

export function SignUpScreen() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const { state, signUp } = useSignUp();

  return (
    <View testID="sign-up-screen" style={styles.container}>
      <Text style={{ fontSize: 24, fontWeight: '600' }}>Create an account</Text>
      <TextInput
        testID="email-input"
        value={email}
        onChangeText={setEmail}
        placeholder="Email"
        autoCapitalize="none"
        keyboardType="email-address"
        style={styles.input}
      />
      <TextInput
        testID="password-input"
        value={password}
        onChangeText={setPassword}
        placeholder="Password"
        secureTextEntry
        style={styles.input}
      />
      <Pressable
        testID="submit-button"
        onPress={() => void signUp(email, password)}
        style={styles.button}
        disabled={state.kind === 'pending'}
      >
        <Text style={styles.buttonText}>{state.kind === 'pending' ? 'Creating…' : 'Sign up'}</Text>
      </Pressable>
      {state.kind === 'error' && (
        <Text testID="auth-error" style={styles.error}>
          Sign up failed. Check your details and try again.
        </Text>
      )}
    </View>
  );
}
