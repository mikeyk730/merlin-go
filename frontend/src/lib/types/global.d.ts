// global.d.ts
declare global {
  interface Uint8ArrayConstructor {
    fromBase64(base64: string): Uint8Array;
    fromHex(hex: string): Uint8Array;
  }
  interface Uint8Array {
    toBase64(): string;
    toHex(): string;
  }
}
// This makes the TypeScript compiler happy, assuming the runtime implementation exists.
export {};