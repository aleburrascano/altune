import { StyleSheet, Text, View } from 'react-native';

// AIDEV-NOTE: Placeholder home screen. Replaced when the first feature with a home
// route lands. Keep here until then so `expo start` boots cleanly during scaffold verification.

export default function HomeScreen() {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>Altune</Text>
      <Text style={styles.subtitle}>Scaffold v0.0.0 — no features yet.</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
    backgroundColor: '#000',
  },
  title: {
    color: '#fff',
    fontSize: 32,
    fontWeight: '600',
    marginBottom: 8,
  },
  subtitle: {
    color: '#888',
    fontSize: 14,
  },
});
