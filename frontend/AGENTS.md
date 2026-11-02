# tmphost Frontend Design System

This document outlines the core design principles and UI patterns to ensure consistency across the tmphost application.

## 1. General Aesthetic

- **Minimalist & High-Density**: Prioritize information and architectural clarity over decorative elements.
- **Flatter Design**: No elevation (drop shadows) on cards or buttons (except for subtle shadow-sm on parent containers).

## 2. Colors

- **Main Background**: `bg-zinc-50` (soft off-white for structural depth).
- **Workspace Canvas**: `bg-white` (pure white for the main interaction areas).
- **Text Primary**: `text-gray-900` or `text-black` for headers.
- **Text Secondary**: `text-gray-500` or `text-gray-400` for meta-info and descriptions.
- **Borders**: `border-gray-100` or `border-gray-200` for architectural separation.
- **Dark mode**: every light colour class gets a `dark:` partner on the zinc scale. The `dark` class on `<html>` comes from `stores/themeStore.js`.
  - Surfaces: `bg-white` → `dark:bg-zinc-900`, `bg-zinc-50` → `dark:bg-zinc-950`, `bg-gray-100` → `dark:bg-zinc-800`.
  - Text: `text-gray-900` → `dark:text-zinc-100`, `text-gray-500` → `dark:text-zinc-400`, `text-gray-400` → `dark:text-zinc-500`.
  - Borders: `border-gray-100/200` → `dark:border-zinc-800`. Hover borders sit one step lighter (`dark:hover:border-zinc-700`).
  - Tints: `bg-rose-50` → `dark:bg-rose-500/10`, `text-rose-600` → `dark:text-rose-400` (amber and emerald work the same way).
  - Primary buttons invert: `bg-black text-white` → `dark:bg-white dark:text-zinc-900`.

## 3. Topography & Components

- **Main Action Buttons**:
  - Rounding: `rounded-lg` (sharp and disciplined, not pill-shaped).
  - Font Size: `text-[11px]` font-black uppercase tracking-widest.
  - Colors: `bg-black` for primary, `bg-white` with `border-gray-200` for secondary.
- **Cards**:
  - Rounding: `rounded-xl`.
  - Border: `border-gray-100` (subtle definition).
  - No Hover-Lift: Do not use `hover:-translate-y-*` or shadow increases on hover. Use border color shifts (`hover:border-gray-200`) or subtle background changes instead.
- **Overall Container**:
  - Main component wrapper should use `rounded-xl` with a `border-gray-200`.

## 4. Layout

- **Centering**: The Login page must be perfectly centered on the screen.
- **Dashboard Aligment**: Workspace-specific pages use left-aligned, high-density layouts mirroring the `hasmcp-app` structure.
- **Paddings**: The content area inside the rounded container is padded once, in `App.vue` (`px-2 pt-4 pb-6 md:p-4`); pages add only their own `px-4` header/section padding. Keep the phone `pb-6`: it is part of the clearance above the floating Menu pill.

## 5. Dialogs & Interactions

- **No Native Modals**: Never use browser `confirm()` or `alert()`.
- **Custom Deletion Flow**: Always use the branded `DeleteModal.vue` component for destructive actions like purging workspaces or deleting tasks.
- **Toast Notifications**: Use the global `useToasts` system for all mission-critical feedback (Success/Error/Info).

## 6. API Data Conventions

- **Naming**: All API data properties must use `camelCase` (e.g., `workspaceId`, `createdAt`).
- **Standard**: Strictly avoid using `snake_case` for any data received from or sent to the backend.

## 7. Application Bootstrap

- **One route table**: routes, the auth guard, the `v-click-outside` directive and the pinia
  setup all live in `src/app.js`, exported as `createtmphostApp({ history, platform })`.
  `src/main.js` (browser) and `desktop/src/renderer/main.js` are thin bootstraps that call it.
- **Never add a route anywhere else.** The desktop app mounts this same table, so a second
  copy would silently let the two builds drift apart. Add the route in `src/app.js` and both
  applications get it.
- **Platform-specific behavior**: read `usePlatformStore()` (`platform`, `isDesktop`, `isWeb`)
  rather than sniffing the user agent or probing for `window.tmphost`. That store is the
  supported seam for an affordance only one build can offer.
- **Browser-only concerns** - the base path the backend injects, the service worker - belong
  in `src/main.js`, not in the shared factory. The desktop build has its own answers for both.
