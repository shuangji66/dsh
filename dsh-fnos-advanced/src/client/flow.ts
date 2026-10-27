/**
 * The per-surface description of one pick attempt.
 *
 * The authorization machinery is shared by the workspace directory picker and
 * the composer intake; a `PickFlow` carries the differences (where the status
 * line goes, what happens with the picked paths, how the button label changes).
 */
export interface PickFlow {
  button: HTMLButtonElement;
  /** fnOS app-auth method used both for the native and the web picker. */
  method: 'pickUserFile';
  directory?: boolean;
  creatable?: boolean;
  /** Whether the picker may return several files (composer intake). */
  multiple?: boolean;
  title: string;
  okText: string;
  /** Element the authorization-entry chooser anchors to. */
  anchor: Element | null;
  /** Report progress / an error on the surface this flow belongs to. */
  status(text: string, isError?: boolean): void;
  /** Reject a path that is already a workspace root. */
  isDuplicate?(paths: string[]): Promise<boolean>;
  /** Consume the picked paths (fill the picker or attach to the composer). */
  onPaths(paths: string[]): Promise<void> | void;
  doneText(paths: string[]): string;
  setLabel(text: string): void;
  doneLabel(): string;
  /** Re-enable the button and restore its label. */
  restore(): void;
}
