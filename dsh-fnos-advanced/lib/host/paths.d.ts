/** Resolved dsh home directory. */
export declare function dshHome(): string;
/** Whether `child` is `parent` or below it (no `..`-free prefix confusion). */
export declare function isInsidePath(child: string, parent: string): boolean;
/** Any `.hidden` segment (`.` / `..` excluded) makes a path unreadable here. */
export declare function hasHiddenSegment(value: string): boolean;
/** `errno` code of an unknown thrown value, if it has one. */
export declare function errorCode(error: unknown, fallback: string): string;
/** A thrown value as display text. */
export declare function errorMessage(error: unknown, fallback: string): string;
