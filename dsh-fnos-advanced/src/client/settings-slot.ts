/**
 * Register the settings page on dsh's `plugins.bundle.config` slot.
 *
 * The bundle-level slot renders directly on the package page, above the
 * component rows; the row-level slot would bury the same form one level deeper.
 */
import { SETTINGS_SLOT_KEY } from './constants.ts';
import type { ClientContext } from './context.ts';
import { SettingsSlot } from './settings-page.tsx';

export function registerSettingsSlot(ctx: ClientContext): void {
  const inject = ctx?.inject;
  if (typeof inject !== 'function') return;

  inject(['slots'], (scope) => {
    const slots = scope.slots;
    if (!slots || typeof slots.inject !== 'function' || typeof slots.register !== 'function') return;

    const install = (): void => {
      slots.inject('plugins.bundle.config', () =>
        slots.register({ name: 'plugins.bundle.config', key: SETTINGS_SLOT_KEY }, SettingsSlot));
    };

    if (typeof scope.effect === 'function') {
      scope.effect(install, 'dsh-fnos-advanced: settings page');
    } else {
      install();
    }
  });
}
