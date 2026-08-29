# Hanxi Workbench Design System

## Design character

Build calm, compact operational interfaces. Put tasks and status ahead of decoration. The interface should feel native to a Windows utility while remaining usable in a browser and on a phone.

- Quiet technical surfaces
- Clear state and action hierarchy
- Moderate information density
- Restrained color and elevation
- Independent light and dark calibration
- Local-first trust expressed through direct copy and immediate feedback, not fixed business labels

## Semantic colors

Use role-based tokens. Components must not depend on theme-specific literals.

| Role | Light | Dark | Purpose |
|---|---:|---:|---|
| `page` | `#f3f7f8` | `#071318` | Application canvas |
| `page-accent` | `#e6f3f2` | `#0b2326` | Optional low-contrast ambient glow |
| `surface` | `#ffffff` | `#0e1c22` | Main panels and cards |
| `surface-soft` | `#f7fafb` | `#12252c` | Nested controls and list rows |
| `surface-hover` | `#eef5f5` | `#173039` | Hover, selected, and drag feedback |
| `border` | `#dce6e8` | `#213840` | Normal boundaries |
| `border-strong` | `#c7d6d9` | `#31505a` | Interactive and emphasized boundaries |
| `text` | `#14252c` | `#e8f2f3` | Primary text |
| `muted` | `#667980` | `#9aadb2` | Supporting copy and metadata |
| `subtle` | `#8a9aa0` | `#748a90` | Non-critical captions |
| `primary` | `#0f8b8d` | `#42b7b4` | Main action, active state |
| `primary-hover` | `#0b7779` | `#59c8c5` | Primary hover/pressed state |
| `primary-soft` | `rgba(15,139,141,.10)` | `rgba(66,183,180,.13)` | Icon wells and subtle emphasis |
| `positive` | `#08875d` | `#47c792` | Healthy/complete/positive data |
| `information` | `#2563eb` | `#72a5ff` | Informational data or secondary identity |
| `warning` | `#b66f08` | `#f0ac45` | Attention and incomplete state |
| `danger` | `#d64545` | `#ff7974` | Errors and destructive actions |

### Surface formula

1. Paint the page with `page`.
2. Place major content on `surface`.
3. Put controls, list rows, and nested regions on `surface-soft`.
4. Move only one level on hover or selection using `surface-hover`.
5. Keep a visible border even when using shadow.

Use a primary soft fill at roughly 10–13% strength. For an emphasized border, mix approximately 25–30% primary into the normal border.

An optional ambient background may use one restrained radial gradient at the viewport edge. Do not place gradients behind dense reading areas.

## Theme calibration

- Define complete semantic tokens for both themes.
- Raise chromatic foreground brightness in dark mode instead of reusing light-mode values.
- Recheck borders and nested surface separation in each theme.
- Use `color-scheme: light dark` when the document owns browser controls.
- Provide light and dark `theme-color` metadata for standalone pages.
- If a host application controls theme with attributes or classes, map these roles into its system rather than adding a competing switch.

## Typography

```css
--font-text: "Segoe UI Variable Text", "Microsoft YaHei UI", "Segoe UI", sans-serif;
--font-display: "Segoe UI Variable Display", "Segoe UI", sans-serif;
--font-mono: ui-monospace, "Cascadia Mono", Consolas, monospace;
```

Use roles rather than copying exact pixels blindly:

| Role | Typical size | Notes |
|---|---:|---|
| Brand/display | 17–20px | Slight negative tracking |
| Section heading | 14–17px | Strong, compact line height |
| Body/input | 14–16px | Critical instructions stay readable |
| Control label/resource name | 12–14px | Medium or semibold |
| Metadata/caption | 10–12px | Only non-critical information |
| Numeric display | 15–19px | Mono and tabular numerals |

Use `font-variant-numeric: tabular-nums` for changing values. Apply monospace only to machine-oriented information: paths, IP addresses, ports, versions, rates, sizes, durations, identifiers, code, and logs.

## Spacing and density

Use a 4px base rhythm with a compact intermediate scale:

```text
2  micro alignment
4  tight inline gap
8  control padding / small gap
12 component internal gap
16 normal section gap
20 large component gap
24 page or major-region padding
```

Values such as 10, 14, and 18px are acceptable for compact tool layouts. Avoid creating a token for every odd number.

- Typical standalone page padding: 18–24px
- Main panel padding: 16–20px
- KPI/card padding: 12–16px
- Resource row minimum height: 52–56px desktop
- Compact button minimum height: 36–38px desktop
- Coarse-pointer controls: at least 44px

Compact density must not reduce touch targets or critical text readability.

## Shape and elevation

Use four radius roles:

```css
--radius-control: 8px;
--radius-element: 12px;
--radius-panel: 16px;
--radius-pill: 999px;
```

- Major panels use the largest radius.
- Nested inputs and rows use one level smaller.
- Icon wells generally match element radius.
- Status badges and progress tracks use pill radius.

Prefer borders before shadow. Two elevation levels are enough:

```css
--shadow-small-light: 0 4px 14px rgba(32, 66, 72, .055);
--shadow-panel-light: 0 18px 45px rgba(32, 66, 72, .085);
--shadow-small-dark: 0 5px 18px rgba(0, 0, 0, .18);
--shadow-panel-dark: 0 22px 50px rgba(0, 0, 0, .23);
```

Use small elevation for KPI cards and panel elevation for primary work areas or Toasts. Do not stack shadows on every nested container.

## Motion and feedback

Motion is short and functional:

| Interaction | Duration | Movement |
|---|---:|---:|
| Button/input/list feedback | ~150ms | 0–1px |
| Progress updates | ~100ms linear | none |
| Toast entry | ≤180ms | ≤6px |
| Spinner | ~750ms linear | continuous only while loading |
| Live status pulse | ~1.8s | opacity only |

Only loading and genuinely live status may animate continuously. Disabled controls must not move on hover.

Always include:

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    scroll-behavior: auto !important;
    animation-duration: .01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: .01ms !important;
  }
}
```

## Accessibility and device behavior

- Keep browser zoom enabled.
- Use `100vh` followed by `100dvh` for standalone full-height pages.
- Apply safe-area padding with `max(base, env(safe-area-inset-*)))`.
- Provide a visible, consistent `:focus-visible` ring.
- Keep critical actions keyboard reachable and discoverable without hover.
- Increase controls to at least 44px in `pointer: coarse` environments.
- Use labels for inputs and accessible names for icon-only buttons.
- Mark decorative SVGs `aria-hidden="true"`.
- Update `aria-valuenow` whenever visual progress changes.
- Use `aria-live` sparingly for meaningful task changes, not every high-frequency metric update.
- Express online, warning, success, and error with text or iconography as well as color.
- Test subtle text and semantic colors for contrast in both themes.
- In Grid and Flex layouts, give text-bearing children `min-width: 0`; use `minmax(0, 1fr)` for flexible columns.
