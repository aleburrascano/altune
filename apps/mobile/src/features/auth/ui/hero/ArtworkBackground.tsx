import { BlurView } from 'expo-blur';
import { LinearGradient } from 'expo-linear-gradient';
import { StyleSheet, View, useWindowDimensions } from 'react-native';

import { radius, useTheme } from '@shared/ui/theme';

function withAlpha(hex: string, alpha: number): string {
  const n = hex.replace('#', '');
  const r = parseInt(n.slice(0, 2), 16);
  const g = parseInt(n.slice(2, 4), 16);
  const b = parseInt(n.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

const COLS = 4;
const ROWS = 4;

export function ArtworkBackground() {
  const theme = useTheme();
  const { width, height } = useWindowDimensions();

  const grid = Math.max(width, height) * 1.7;
  const cell = grid / COLS;
  const canvas = theme.color.canvas;

  const [heroA, heroB] = theme.color.heroGradient;
  const pairs: [string, string][] = [
    [theme.color.accent, heroB],
    [theme.color.success, theme.color.accent],
    [theme.color.warning, theme.color.danger],
    [theme.color.danger, heroA],
    [heroB, theme.color.success],
    [theme.color.warning, theme.color.success],
    [theme.color.accent, theme.color.danger],
    [theme.color.success, theme.color.warning],
  ];

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="none">
      <View
        style={{
          position: 'absolute',
          top: (height - grid) / 2 - height * 0.16,
          left: (width - grid) / 2,
          width: grid,
          height: grid,
          flexDirection: 'row',
          flexWrap: 'wrap',
          transform: [{ rotate: '-8deg' }, { scale: 1.06 }],
        }}
      >
        {Array.from({ length: COLS * ROWS }).map((_, i) => {
          const [start, end] = pairs[i % pairs.length] ?? [theme.color.accent, theme.color.accent];
          return (
            <View key={i} style={{ width: cell, height: cell, padding: 4 }}>
              <LinearGradient
                colors={[start, end]}
                start={{ x: 0, y: 0 }}
                end={{ x: 1, y: 1 }}
                style={{ flex: 1, borderRadius: radius.md, opacity: 0.85 }}
              />
            </View>
          );
        })}
      </View>
      <BlurView intensity={48} tint="dark" style={StyleSheet.absoluteFill} />
      <LinearGradient
        colors={[withAlpha(canvas, 0.3), withAlpha(canvas, 0.5), withAlpha(canvas, 0.96), canvas]}
        locations={[0, 0.42, 0.78, 1]}
        style={StyleSheet.absoluteFill}
      />
    </View>
  );
}
