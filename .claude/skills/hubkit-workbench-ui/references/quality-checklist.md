# HubKit Workbench UI Quality Checklist

Run this checklist before declaring substantial HubKit UI work complete. Check only states and patterns that apply to the task.

## Task fit

- [ ] The page has a clear primary task or operational purpose.
- [ ] KPI cards contain real decision-relevant information.
- [ ] Panels and lists match the business structure rather than copying the fileshare layout.
- [ ] No fileshare API, copy, DOM ID, upload flow, or unrelated interaction was copied.
- [ ] Unnecessary template sections were removed.

## Architecture

- [ ] Existing framework and component-library conventions are preserved.
- [ ] Vue props, emits, routes, bindings, composables, events, and lifecycle behavior remain correct.
- [ ] Plain HTML follows the target module's existing asset and serving structure.
- [ ] Business logic was not rewritten merely to fit a visual pattern.
- [ ] Shared components were reused where appropriate; no parallel component system was introduced.

## Visual system

- [ ] Page, panel, nested, and hover/selected surface levels are distinguishable.
- [ ] Light and dark themes were checked independently.
- [ ] Fine borders establish hierarchy before shadows.
- [ ] Radius levels are restrained and consistent.
- [ ] Machine-oriented values use mono/tabular numerals where useful.
- [ ] Primary, secondary, informational, warning, and destructive roles are visually distinct.
- [ ] Status is not communicated by color alone.

## State matrix

For each asynchronous or stateful region, verify applicable states:

- [ ] Initial
- [ ] Loading
- [ ] Success
- [ ] Empty
- [ ] Stale
- [ ] Error
- [ ] Disabled/busy
- [ ] Partial progress
- [ ] Partial success
- [ ] Cancelled
- [ ] Retry/recovery

- [ ] Important errors remain visible in context; Toast is not the only feedback.
- [ ] Batch results do not claim total success after individual failures.
- [ ] Busy controls keep stable dimensions and prevent duplicate actions.

## Responsive behavior

Check approximately:

- [ ] 1280px desktop
- [ ] 768px compact/tablet
- [ ] 390px phone
- [ ] 200% browser zoom where practical

Also verify:

- [ ] Long titles, paths, addresses, identifiers, and resource names do not break the page.
- [ ] KPI layouts work with one, two, three, and four real metrics.
- [ ] Task areas work with one or two real panels.
- [ ] Resource rows transform structurally on narrow screens.
- [ ] The page body does not scroll horizontally.
- [ ] Wide tables, logs, and code scroll inside their own containers.
- [ ] Touch targets remain usable in coarse-pointer environments.

## Accessibility

- [ ] All essential actions are keyboard reachable.
- [ ] `:focus-visible` is clear in both themes.
- [ ] Inputs have labels; icon-only controls have accessible names.
- [ ] Decorative SVGs are hidden from assistive technology.
- [ ] Progress visuals and ARIA values stay synchronized.
- [ ] Live regions announce meaningful changes without repeating high-frequency metrics.
- [ ] Hover is not the only way to discover an action.
- [ ] Browser zoom remains enabled.
- [ ] Reduced-motion preference disables non-essential animation.
- [ ] Safe-area insets and dynamic viewport height are handled for standalone mobile pages.
- [ ] Subtle text and semantic colors have sufficient contrast.

## Interaction quality

- [ ] Hover styles do not cause meaningful layout movement.
- [ ] Disabled controls are visually and functionally disabled.
- [ ] Destructive actions are separated and confirmed when hard to reverse.
- [ ] Empty and error states explain the next step.
- [ ] Loading text names the resource or operation being loaded.
- [ ] Focus returns to a sensible location after dialogs, errors, or completed operations where relevant.

## Final restraint pass

- [ ] Remove one unnecessary decorative element if the page feels busy.
- [ ] Remove duplicated explanation or status text.
- [ ] Remove any ornamental gradient, heavy shadow, or animation without a functional purpose.
- [ ] Confirm the page still looks like the target module, not a copied demo.
- [ ] Run the normal build, type check, tests, and real application/page check.
