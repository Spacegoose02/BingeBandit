import { NativeModules } from 'react-native';

const AudioSupport = NativeModules.AudioSupport as
  | { playableCodecs(): Promise<string[]> }
  | undefined;

let playable: Set<string> | undefined;

/**
 * Which surround formats (ac3, eac3, dts, truehd) this device can play, by a
 * decoder of its own or HDMI passthrough. Asked again for each video, since
 * the TV or receiver on the other end of the HDMI cable can change.
 */
export async function refreshAudioSupport(): Promise<Set<string>> {
  try {
    playable = new Set(AudioSupport ? await AudioSupport.playableCodecs() : []);
  } catch {
    playable = new Set();
  }
  return playable;
}

/** The last answer from refreshAudioSupport (none yet: assume no surround formats). */
export function surroundPlayable(codec: string): boolean {
  return playable?.has(codec) ?? false;
}
