/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * Theme customization constants and types.
 *
 * Lives in `lib/` (not `context/`) so it can be imported alongside the
 * provider without breaking React Fast Refresh boundaries.
 */

export const THEME_PRESETS = [
  {
    value: 'default',
    name: 'Default',
    swatches: ['oklch(0.546 0.215 262)', 'oklch(0.63 0.145 222)'],
  },
] as const

export type ThemePreset = (typeof THEME_PRESETS)[number]['value']
export type ThemeRadius = 'default' | 'none' | 'sm' | 'md' | 'lg' | 'xl'
export type ThemeScale = 'default' | 'sm' | 'lg' | 'xl'
export type ContentLayout = 'full' | 'centered'

/**
 * Font axis for the theme.
 *
 * - `default` — resolve at runtime from `PRESET_DEFAULT_FONT`, which maps
 *   the shipped `default` preset to the humanist sans.
 * - `sans` — humanist sans (Public Sans), the project's UI fallback.
 * - `serif` — editorial serif (Lora + CJK fallbacks), the project's
 *   "soul" typography. Inherits across the whole UI; monospace contexts
 *   keep their own family via Tailwind preflight and `.font-mono`.
 */
export type ThemeFont = 'default' | 'sans' | 'serif'

/**
 * The resolved (non-`default`) font value applied to the DOM. The provider
 * always sets `data-theme-font` to one of these concrete values so CSS only
 * needs simple attribute selectors (no `:not()` gymnastics, no per-preset
 * font branches).
 */
export type ResolvedThemeFont = Exclude<ThemeFont, 'default'>

export type ThemeCustomization = {
  preset: ThemePreset
  font: ThemeFont
  radius: ThemeRadius
  scale: ThemeScale
  contentLayout: ContentLayout
}

export const DEFAULT_THEME_CUSTOMIZATION: ThemeCustomization = {
  preset: 'default',
  font: 'default',
  radius: 'default',
  scale: 'default',
  contentLayout: 'full',
}

export const THEME_PRESET_VALUES = new Set(
  THEME_PRESETS.map((p) => p.value)
) as ReadonlySet<ThemePreset>

export const THEME_FONT_VALUES: ReadonlySet<ThemeFont> = new Set([
  'default',
  'sans',
  'serif',
])

export const THEME_RADIUS_VALUES: ReadonlySet<ThemeRadius> = new Set([
  'default',
  'none',
  'sm',
  'md',
  'lg',
  'xl',
])

export const THEME_SCALE_VALUES: ReadonlySet<ThemeScale> = new Set([
  'default',
  'sm',
  'lg',
  'xl',
])

export const CONTENT_LAYOUT_VALUES: ReadonlySet<ContentLayout> = new Set([
  'full',
  'centered',
])

export const THEME_COOKIE_KEYS = {
  preset: 'theme_preset',
  font: 'theme_font',
  radius: 'theme_radius',
  scale: 'theme_scale',
  contentLayout: 'theme_content_layout',
} as const

/**
 * Preset → default font mapping. Used by the provider to resolve the user's
 * `font: 'default'` preference against the active preset.
 *
 * Only the `default` preset ships; it resolves to the humanist sans so the
 * out-of-the-box experience stays clean and readable.
 */
export const PRESET_DEFAULT_FONT: Partial<
  Record<ThemePreset, ResolvedThemeFont>
> = {
  default: 'sans',
}

/**
 * Resolve a user font preference + active preset into the concrete font that
 * should drive the DOM. Pure function so it's safe to call inside both the
 * effect that applies the attribute and the UI preview that hints at what
 * `default` will render as.
 */
export function resolveThemeFont(
  font: ThemeFont,
  preset: ThemePreset
): ResolvedThemeFont {
  if (font === 'default') {
    return PRESET_DEFAULT_FONT[preset] ?? 'sans'
  }
  return font
}
