import { Link } from 'expo-router';
import { Text, View } from 'react-native';

// Stub. Real form lands in Slice 11.
export default function SignInScreen() {
  return (
    <View testID="sign-in-screen" style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: 16 }}>
      <Text>Sign in</Text>
      <Link href="/sign-up" testID="link-to-sign-up">
        <Text>No account? Sign up</Text>
      </Link>
    </View>
  );
}
