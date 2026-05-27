import { Link } from 'expo-router';
import { Text, View } from 'react-native';

// Stub. Real form lands in Slice 12.
export default function SignUpScreen() {
  return (
    <View testID="sign-up-screen" style={{ flex: 1, alignItems: 'center', justifyContent: 'center', gap: 16 }}>
      <Text>Sign up</Text>
      <Link href="/sign-in" testID="link-to-sign-in">
        <Text>Have an account? Sign in</Text>
      </Link>
    </View>
  );
}
