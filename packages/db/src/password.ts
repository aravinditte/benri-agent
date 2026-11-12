import { createHash, randomBytes, scryptSync, timingSafeEqual } from "node:crypto";
export function hashPassword(password: string): string {
  const salt = randomBytes(16);
  const hash = scryptSync(password, salt, 32);
  return `scrypt:${salt.toString("hex")}:${hash.toString("hex")}`;
}
export function verifyPassword(password: string, stored: string): boolean {
  const parts = stored.split(":");
  if (parts.length !== 3 || parts[0] !== "scrypt") return false;
  const expected = Buffer.from(parts[2], "hex");
  const actual = scryptSync(password, Buffer.from(parts[1], "hex"), 32);
  return expected.length === actual.length && timingSafeEqual(expected, actual);
}
export function hashToken(token: string): string {
  return createHash("sha256").update(token).digest("hex");
}
export function contentHash(text: string): string {
  return createHash("sha256").update(text).digest("hex");
}
