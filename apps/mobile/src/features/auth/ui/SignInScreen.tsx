import { Link } from 'expo-router';
import { useState } from 'react';
import { Pressable, Text, TextInput, View } from 'react-native';

import { useSignIn } from '../hooks/useSignIn';

const styles = {
  container: {
    flex: 1,
    padding: 24,
    justifyContent: 'center' as const,
    gap: 12,
    backgroundColor: '#fff',
  },
  title: { fontSize: 24, fontWeight: '600' as const, color: '#111' },
  input: {
    borderWidth: 1,
    borderColor: '#888',
    padding: 12,
    borderRadius: 6,
    color: '#111',
    backgroundColor: '#fff',
  },
  button: {
    backgroundColor: '#222',
    padding: 14,
    borderRadius: 6,
    alignItems: 'center' as const,
  },
  buttonText: { color: '#fff' },
  error: { color: '#c00' },
};

export function SignInScreen() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const { state, signIn } = useSignIn();

  return (
    <View testID="sign-in-screen" style={styles.container}>
      <Text style={styles.title}>Sign in</Text>
      <TextInput
        testID="email-input"
        value={email}
        onChangeText={setEmail}
        placeholder="Email"
        placeholderTextColor="#888"
        autoCapitalize="none"
        keyboardType="email-address"
        style={styles.input}
      />
      <TextInput
        testID="password-input"
        value={password}
        onChangeText={setPassword}
        placeholder="Password"
        placeholderTextColor="#888"
        secureTextEntry
        style={styles.input}
      />
      <Pressable
        testID="submit-button"
        onPress={() => void signIn(email, password)}
        style={styles.button}
        disabled={state.kind === 'pending'}
      >
        <Text style={styles.buttonText}>{state.kind === 'pending' ? 'Signing in…' : 'Sign in'}</Text>
      </Pressable>
      <Link href="/sign-up" testID="link-to-sign-up">
        <Text style={{ color: '#06f', textAlign: 'center' }}>No account? Sign up</Text>
      </Link>
      {state.kind === 'error' && (
        <Text testID="auth-error" style={styles.error}>
          Sign in failed. Check your details and try again.
        </Text>
      )}
    </View>
  );
}
