---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native accessibility — labels, touch targets, screen reader, contrast, state

## Labels

Every interactive element MUST have accessibility metadata:

```tsx
// GOOD — full accessibility props
<Pressable
  onPress={onPlay}
  accessibilityLabel="Play track"
  accessibilityRole="button"
  accessibilityHint="Starts playing the current track"
>
  <PlayIcon />
</Pressable>

// BAD — bare pressable with no accessibility info
<Pressable onPress={onPlay}>
  <PlayIcon />
</Pressable>
```

## Touch targets

- 44x44pt minimum for all interactive elements
- Use `hitSlop` to expand small visual elements to meet the minimum
- 8pt minimum spacing between adjacent touch targets

## Screen reader

- Test with VoiceOver (iOS) and TalkBack (Android) on real devices
- Ensure logical focus order — tab order should match visual reading order
- Hide purely decorative elements with `accessibilityElementsHidden` or `importantForAccessibility="no"`
- Announce dynamic content changes with `AccessibilityInfo.announceForAccessibility`

## Color and contrast

- 4.5:1 contrast ratio minimum (WCAG AA)
- Never rely on color alone to convey information
- Support both light and dark mode
- Test with the "Increase Contrast" accessibility setting enabled

## State communication

```tsx
// GOOD — full accessibility state on a toggle
<Switch
  value={isEnabled}
  onValueChange={setIsEnabled}
  accessibilityLabel="Enable notifications"
  accessibilityRole="switch"
  accessibilityState={{ checked: isEnabled }}
/>
```

- Use `accessibilityState` for disabled, selected, checked, expanded states
- Use `accessibilityValue` for sliders, progress bars, and numeric inputs
- Mark required form fields explicitly
