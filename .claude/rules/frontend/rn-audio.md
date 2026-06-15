---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native Audio API (react-native-audio-api)

Low-level Web Audio API implementation for React Native. Use for real-time audio processing, synthesis, effects chains, and custom audio graphs. For simple playback, prefer `expo-audio`.

## AudioContext creation and lifecycle

```tsx
import { AudioContext } from 'react-native-audio-api';

const audioContext = new AudioContext();

// Properties
audioContext.currentTime;    // seconds elapsed since creation
audioContext.sampleRate;     // e.g. 44100 or 48000
audioContext.state;          // 'running' | 'suspended' | 'closed'
audioContext.destination;    // final output node (speakers)

// Lifecycle
await audioContext.resume();   // resume after suspend
await audioContext.suspend();  // pause processing (battery saver)
await audioContext.close();    // release resources — cannot reuse
```

## Audio nodes

Nodes are created from the AudioContext and connected in a graph. Signal flows from source -> processing -> destination.

### GainNode (volume control)

```tsx
const gain = audioContext.createGain();
gain.gain.value = 0.5;             // 0 = silence, 1 = full
gain.gain.setValueAtTime(0.8, audioContext.currentTime + 1);

source.connect(gain);
gain.connect(audioContext.destination);
```

### OscillatorNode (tone generator)

```tsx
const osc = audioContext.createOscillator();
osc.type = 'sine';       // 'sine' | 'square' | 'sawtooth' | 'triangle'
osc.frequency.value = 440;  // Hz (A4)
osc.detune.value = 0;       // cents

osc.connect(audioContext.destination);
osc.start();
osc.stop(audioContext.currentTime + 2);  // stop after 2 seconds
```

### BiquadFilterNode (EQ, lowpass, highpass)

```tsx
const filter = audioContext.createBiquadFilter();
filter.type = 'lowpass';  // 'lowpass' | 'highpass' | 'bandpass' | 'notch' | 'allpass' | 'peaking' | 'lowshelf' | 'highshelf'
filter.frequency.value = 1000;  // cutoff frequency
filter.Q.value = 1;             // resonance
filter.gain.value = 0;          // dB (for peaking/shelf types)
```

### DelayNode

```tsx
const delay = audioContext.createDelay(5.0);  // max delay in seconds
delay.delayTime.value = 0.3;                  // current delay in seconds
```

### StereoPannerNode

```tsx
const panner = audioContext.createStereoPanner();
panner.pan.value = -1;  // -1 = full left, 0 = center, 1 = full right
```

### WaveShaperNode (distortion)

```tsx
const shaper = audioContext.createWaveShaper();
const curve = new Float32Array(256);
for (let i = 0; i < 256; i++) {
  const x = (i * 2) / 256 - 1;
  curve[i] = Math.tanh(x * 3);  // soft clipping
}
shaper.curve = curve;
shaper.oversample = '4x';  // 'none' | '2x' | '4x'
```

### AnalyserNode (visualization)

```tsx
const analyser = audioContext.createAnalyser();
analyser.fftSize = 2048;
analyser.smoothingTimeConstant = 0.8;

const dataArray = new Float32Array(analyser.frequencyBinCount);
analyser.getFloatFrequencyData(dataArray);    // frequency domain (dB)
analyser.getFloatTimeDomainData(dataArray);   // time domain (-1 to 1)

const byteArray = new Uint8Array(analyser.frequencyBinCount);
analyser.getByteFrequencyData(byteArray);     // 0-255 frequency
analyser.getByteTimeDomainData(byteArray);    // 0-255 waveform
```

### ConvolverNode (reverb via impulse response)

```tsx
const convolver = audioContext.createConvolver();
const impulseResponse = await audioContext.decodeAudioData(irArrayBuffer);
convolver.buffer = impulseResponse;
convolver.normalize = true;
```

## Loading and playing audio

```tsx
// Fetch audio file -> decode -> play
const response = await fetch('https://example.com/track.mp3');
const arrayBuffer = await response.arrayBuffer();
const audioBuffer = await audioContext.decodeAudioData(arrayBuffer);

const source = audioContext.createBufferSource();
source.buffer = audioBuffer;
source.loop = false;
source.playbackRate.value = 1.0;
source.connect(audioContext.destination);
source.start();                              // play immediately
source.start(audioContext.currentTime + 1);  // play after 1 second
source.stop(audioContext.currentTime + 5);   // stop after 5 seconds
```

BufferSourceNode is one-shot — create a new one for each playback.

## AudioParam automation

```tsx
const gain = audioContext.createGain();
const now = audioContext.currentTime;

// Instant set
gain.gain.setValueAtTime(0, now);

// Linear ramp
gain.gain.linearRampToValueAtTime(1, now + 0.5);   // fade in over 0.5s

// Exponential ramp (value must be > 0)
gain.gain.exponentialRampToValueAtTime(0.01, now + 2);

// Exponential approach (asymptotic, never reaches target exactly)
gain.gain.setTargetAtTime(0.5, now, 0.1);  // target, startTime, timeConstant

// Cancel scheduled values
gain.gain.cancelScheduledValues(now);
gain.gain.cancelAndHoldAtTime(now);  // cancel but hold current value
```

## Custom audio processing with worklets

```tsx
import { AudioContext } from 'react-native-audio-api';

// Register processor
await audioContext.audioWorklet.addModule(`
  class WhiteNoiseProcessor extends AudioWorkletProcessor {
    process(inputs, outputs, parameters) {
      const output = outputs[0];
      for (let channel = 0; channel < output.length; channel++) {
        for (let i = 0; i < output[channel].length; i++) {
          output[channel][i] = Math.random() * 2 - 1;
        }
      }
      return true;  // keep running
    }
  }
  registerProcessor('white-noise', WhiteNoiseProcessor);
`);

const noiseNode = new AudioWorkletNode(audioContext, 'white-noise');
noiseNode.connect(audioContext.destination);
```

## Recording with AudioRecorder

```tsx
import { AudioRecorder } from 'react-native-audio-api';

const recorder = new AudioRecorder();

await recorder.start();
const blob = await recorder.stop();
// blob contains recorded audio data
```

## Common patterns

### Effects chain

```tsx
const source = audioContext.createBufferSource();
const filter = audioContext.createBiquadFilter();
const gain = audioContext.createGain();
const panner = audioContext.createStereoPanner();

// Chain: source -> filter -> gain -> panner -> speakers
source.connect(filter);
filter.connect(gain);
gain.connect(panner);
panner.connect(audioContext.destination);
```

### Synthesizer note

```tsx
function playNote(frequency: number, duration: number) {
  const osc = audioContext.createOscillator();
  const gain = audioContext.createGain();
  const now = audioContext.currentTime;

  osc.frequency.value = frequency;
  osc.type = 'sawtooth';

  // ADSR envelope (attack-decay-sustain-release)
  gain.gain.setValueAtTime(0, now);
  gain.gain.linearRampToValueAtTime(0.8, now + 0.01);   // attack
  gain.gain.linearRampToValueAtTime(0.4, now + 0.1);    // decay to sustain
  gain.gain.linearRampToValueAtTime(0, now + duration);  // release

  osc.connect(gain);
  gain.connect(audioContext.destination);
  osc.start(now);
  osc.stop(now + duration);
}
```

## Performance rules

- **Single AudioContext** — create one and reuse. Multiple contexts waste resources.
- **Use AudioParam automation** (`setValueAtTime`, ramps) instead of setting `.value` in requestAnimationFrame — automation runs on the audio thread, `.value` causes glitches from JS thread scheduling.
- **Keep worklets lean** — `process()` runs per audio frame (~128 samples at ~344 Hz). No allocations, no async, no closures over large objects.
- **Disconnect unused nodes** — call `node.disconnect()` when done to let GC reclaim.
- **BufferSource is one-shot** — create a new `createBufferSource()` for each playback; reuse the decoded `AudioBuffer`.
- **Close context on unmount** — call `audioContext.close()` in cleanup to release native resources.
