/**
 * Filesystem locations shared by the host routes.
 *
 * Everything the plugin reads or writes lives under dsh's own home; the location
 * follows `DSH_HOME` because dsh itself does (Harness may run with a home other
 * than `~/.dsh`).
 */
import { homedir } from 'node:os';
import { join, resolve, sep } from 'node:path';

/** Resolved dsh home directory. */
export function dshHome(): string {
  return resolve(process.env.DSH_HOME || join(homedir(), '.dsh'));
}

/** Whether `child` is `parent` or below it (no `..`-free prefix confusion). */
export function isInsidePath(child: string, parent: string): boolean {
  return child === parent || child.startsWith(parent.endsWith(sep) ? parent : `${parent}${sep}`);
}

/** Any `.hidden` segment (`.` / `..` excluded) makes a path unreadable here. */
export function hasHiddenSegment(value: string): boolean {
  return value.split(sep).some((segment) => segment.startsWith('.') && segment !== '.' && segment !== '..');
}

/** `errno` code of an unknown thrown value, if it has one. */
export function errorCode(error: unknown, fallback: string): string {
  const code = (error as NodeJS.ErrnoException | null | undefined)?.code;
  return typeof code === 'string' && code ? code : fallback;
}

/** A thrown value as display text. */
export function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}
