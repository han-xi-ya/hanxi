# Hanxi Workbench Component Patterns

Choose only the patterns justified by the task. A simple view does not become better by receiving every pattern in this document.

## Workbench shell

Use a centered, finite-width work area for standalone tools. A typical maximum is 1080–1160px.

Structure the page as:

1. Global header: identity, current operating state, one or two global actions.
2. Optional operational overview.
3. Primary task area.
4. Managed resources or detailed workspace.
5. Minimal environment or safety note.

Keep the header compact. Avoid marketing-style hero sections unless the user explicitly requests one.

On narrow screens, hide secondary brand copy before hiding current state or essential actions. Icon-only actions must retain accessible names.

## KPI grid: 4 → 2 → 1

Use KPI tiles only for decision-relevant status. Do not invent metrics to fill a grid.

Each tile uses:

- Category icon or status mark
- Short label
- Primary value in mono/tabular numerals when appropriate
- One contextual line

Recommended transformation:

| Environment | Composition |
|---|---|
| Wide desktop | Up to four columns |
| Tablet/compact desktop | Two columns |
| Narrow phone | One column |

One tile may receive subtle primary emphasis when it represents overall health. Emphasis is optional, not a requirement.

For two or three metrics, use the real count instead of empty placeholder tiles.

## Primary task grid: 2 → 1

Use two columns when the page genuinely offers two peer tasks, such as create/configure, send/receive, inspect/operate, or input/select.

Each task panel follows:

```text
Heading: icon + title + one-sentence purpose
Body: primary input or operation surface
Footer: supporting hint on the left, primary action on the right
```

- Let the body grow so panel footers align on desktop.
- Collapse to one column on narrow screens.
- When one task is unavailable, let the remaining task occupy full width.
- Do not give destructive and primary actions equal visual weight.

## Status badge

Use for running, connected, synchronized, stale, warning, or failed states.

A badge combines:

- A shape or dot
- Explicit state text
- Optional icon
- A quiet surface and border

Animate only a truly active/live state. Stale or offline states should become static and neutral or warning-colored. Never show a pulsing dot with no matching live behavior.

## Operation panel

Use for forms, service controls, target selection, command execution, or settings groups.

- Keep one obvious primary action per operation group.
- Put supporting context near the relevant control, not in a distant help block.
- Preserve button width while busy to avoid layout movement.
- Make disabled state both visual and functional.
- Separate destructive operations spatially and semantically; add confirmation when consequences are hard to reverse.
- Group related settings into nested soft surfaces rather than many independent floating cards.

## Dropzone or resource selector

Use a `<label>` linked to a file input or a real button that opens the selector.

Required states:

- Default
- Hover/focus
- Dragover where drag is supported
- Disabled/busy
- Error or limit message

Coarse-pointer devices must have a normal tap-to-select path and must not depend on drag. State the actual supported capability instead of using generic promotional copy.

## Long-task progress

Use for uploads, downloads, installations, scans, migrations, or command execution.

Show the applicable fields:

- Task or resource name
- Current stage
- Percentage for determinate work
- Completed/total units or bytes
- Optional speed or elapsed time
- Cancel/retry action
- Batch summary where relevant

Keep visual progress and ARIA progress synchronized. For indeterminate work, do not fake a percentage. On failure or cancellation, retain enough context to understand what happened and what can be retried.

## Breadcrumb or hierarchy navigation

Use a semantic `<nav>` for paths, scopes, or nested resources.

- Make ancestors real buttons or links.
- Distinguish the current item from clickable ancestors.
- Allow horizontal scrolling on narrow screens instead of breaking every segment across lines.
- Build paths with DOM or framework bindings, never inline JavaScript strings.
- For very deep paths, collapse middle segments while preserving the root and current context.

## Resource list

Use for files, ports, processes, devices, versions, jobs, rules, logs, or other managed entities.

A row separates:

1. Primary action/content: icon, name, short description.
2. Metadata/status.
3. Secondary row actions.

Recommended desktop layout:

```css
grid-template-columns: minmax(0, 1fr) auto;
```

Required behaviors:

- Long names truncate or wrap intentionally.
- Metadata uses mono only when machine-oriented.
- The main row action and secondary buttons remain separate; do not nest buttons inside a clickable row.
- Narrow screens switch to a vertical composition rather than squeezing columns.
- Row actions remain keyboard reachable and do not appear only on hover.

## Loading, empty, stale, error, and retry

Use one shared state-container grammar: quiet nested surface, subtle or dashed boundary, clear title, concise explanation, optional recovery action.

### Loading

Say what is loading. Use a spinner or skeleton only while the request is active.

### Empty

Explain why the area is empty and what the user can do next. Avoid stopping at “暂无数据”.

### Stale

Keep the last valid data when useful and label it as stale or temporarily unavailable. Do not communicate staleness only by lowering opacity.

### Error

Name the failed operation, retain useful context, and provide retry or corrective guidance where possible.

### Partial success

For batch operations, report success and failure counts rather than claiming complete success after individual failures.

## Responsive composition

Transform structure before reducing readability.

| Pattern | Wide | Medium | Narrow |
|---|---|---|---|
| KPI grid | 4 columns | 2 columns | 1 column |
| Primary tasks | 2 columns | 1–2 columns by content | 1 column |
| Header | Brand + state + actions | Same with reduced copy | Hide secondary copy; keep essential action |
| Resource row | Main + metadata/actions | Tighter main + actions | Vertical row |
| Panel footer | Hint + action | Same if space allows | Vertical; primary action full width |
| Breadcrumb | Inline | Scrollable if needed | Horizontally scrollable |

Use `pointer: coarse` for touch sizing rather than assuming every narrow screen is touch-enabled.

## Vue mapping

- Map patterns to existing components and reactive state.
- Use `v-if`/`v-else`, computed state, and event bindings instead of DOM class mutation.
- Preserve existing composables and feedback mechanisms.
- Avoid extracting one-off visual wrappers as global components.

## Plain HTML mapping

- Use semantic elements and event listeners.
- Keep data values in `textContent` and safe attributes.
- Separate styles and scripts according to the target serving model.
- Use the asset only as a starting proportion; remove every irrelevant section.
