---
name: hubkit-workbench-ui
description: Apply HubKit's calm, compact workbench design language when creating, redesigning, reviewing, or substantially extending a HubKit Vue view, embedded HTML page, status screen, operation console, resource list, dashboard, or responsive tool interface. Use whenever interface structure, visual hierarchy, interaction states, responsive behavior, themes, or accessibility require design decisions. Do not use for backend-only work, business-logic-only fixes, documentation edits, or tiny style corrections.
---

# HubKit Workbench UI

Build HubKit interfaces as calm, compact, local-first workbenches. Preserve the target module's architecture and business contract while applying a shared visual character: cool layered surfaces, a restrained teal primary color, fine borders, low-noise elevation, clear operational status, dense but readable information, and deliberate mobile behavior.

## Source and priority

Apply requirements in this order:

1. The user's explicit task and acceptance criteria.
2. The target module's framework, component library, data flow, and interaction contract.
3. [`references/design-system.md`](references/design-system.md).
4. [`references/component-patterns.md`](references/component-patterns.md).
5. [`assets/responsive-workbench.html`](assets/responsive-workbench.html) as a low-priority structural starter only.

The design language was calibrated from:

- `internal/modules/fileshare/web/assets/app.css`
- `internal/modules/fileshare/web/index.html`

Treat the references in this skill as the normal design specification. Re-read the source implementation only when recalibrating the skill or when changing that page itself.

## Required workflow

1. Read the target view and adjacent components before choosing a layout.
2. Identify the page's primary task, managed resource, important status data, destructive actions, and asynchronous states.
3. Decide whether the work is a new page, substantial redesign, structural extension, or UI review.
4. Read `references/design-system.md` when choosing tokens, themes, density, typography, motion, or device behavior.
5. Read `references/component-patterns.md` when composing KPI areas, task panels, status badges, progress, navigation, resource rows, or state containers.
6. Preserve existing API calls, bindings, props, emits, routes, events, and lifecycle behavior unless the task explicitly changes them.
7. Design structure and state behavior before styling. Do not start by pasting the template and forcing the business into it.
8. Implement every applicable loading, empty, stale, error, disabled, progress, success, and cancelled state.
9. Read `references/quality-checklist.md` before final verification.
10. Run the target module's normal build, type check, tests, and real UI check when available.

## Core direction

- Prefer tool-like clarity over marketing-page decoration.
- Use four cool surface levels: page, panel, nested control, and hover/selected feedback.
- Use a low-saturation teal for primary interaction and active state.
- Calibrate light and dark themes independently; do not mechanically invert colors.
- Let fine borders establish hierarchy before adding shadow.
- Use compact typography without shrinking critical text or touch targets.
- Use monospace only for machine-oriented values such as paths, addresses, ports, versions, rates, sizes, durations, and logs.
- Separate global status, primary tasks, managed resources, and supporting metadata.
- Use short functional motion, generally 100–180ms and only a few pixels of movement.
- Express status through text and structure as well as color.

## Framework adaptation

### Vue 3 views

- Reuse existing SFCs, shared components, composables, global tokens, Tailwind utilities, and Element Plus conventions where present.
- Translate the patterns into Vue templates and reactive state. Do not paste the standalone HTML asset into an SFC.
- Represent loading, empty, error, disabled, stale, and progress states through Vue state rather than direct DOM manipulation.
- Preserve Wails bindings, props, emits, routing, error handling, and lifecycle cleanup.
- Extract a component only when reuse or complexity justifies it; do not create a parallel component system for one page.
- Keep scoped CSS focused on page composition. Reuse existing project-level semantic variables when they already express the required role.

### Standalone HTML interfaces

- Use the HTML asset only for an embedded, no-build interface where a plain document is appropriate.
- Replace every neutral sample with the target business structure and remove unused patterns.
- Split HTML, CSS, and JavaScript according to the target module's current serving and embedding approach.
- Prefer semantic HTML and progressive enhancement.
- Use real buttons, links, labels, progress semantics, and DOM event listeners.
- Keep dynamic values out of inline handlers and untrusted `innerHTML`.

## Non-negotiable boundaries

- Do not copy fileshare API paths, upload/download flows, polling, DOM IDs, event functions, resource names, or business copy.
- Do not assume every page needs four KPI cards, two task panels, a dropzone, breadcrumb, or resource list.
- Do not force a simple settings page into a dashboard.
- Do not change business logic merely to fit the visual pattern.
- Do not use emoji as the primary icon system. Reuse the project's icons or use restrained inline SVG.
- Do not use color as the only state indicator.
- Do not make dark mode a simple color inversion.
- Do not use large decorative gradients, glassmorphism, heavy stacked shadows, or ornamental animation.
- Do not preserve desktop layout on mobile by only shrinking text.
- Do not hide essential actions behind hover-only behavior.
- Do not disable browser zoom.

## Resource selection

- Read [`references/design-system.md`](references/design-system.md) for semantic tokens, typography, spacing, themes, motion, and accessibility foundations.
- Read [`references/component-patterns.md`](references/component-patterns.md) for composition patterns and responsive transformations.
- Read [`references/quality-checklist.md`](references/quality-checklist.md) immediately before declaring UI work complete.
- Use [`assets/responsive-workbench.html`](assets/responsive-workbench.html) only as a neutral proportion and semantics reference for new standalone pages. Delete any section that does not fit the actual task.
