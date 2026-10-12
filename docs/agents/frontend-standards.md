# Frontend coding standards

## Dropdown selection

Use `ChoicePicker` from `web/src/ui/ChoicePicker.tsx` for dropdown value
selection. Its shared stylesheet, `web/src/ui/choice-picker.css`, owns the
trigger border, rounded menu, selected checkmark, hover/focus states and
scrolling. Pagination and Playground model selection are reference callers.

Pass a localized accessible `label`, controlled `value`, unique-valued `items`
and `onChange`. Use `disabled` during unavailable operations. `placement`
controls whether the menu opens above or below the trigger; it defaults to
`bottom`. Caller classes may set width, flex or grid placement, but visual
changes belong in the shared stylesheet and apply to every caller.

Use this component instead of native `<select>` controls or copied dropdown
markup/styles. Extend the shared component for new selection requirements;
record any necessary exception and its reason in the PR for review. Action
menus, which execute commands rather than select a value, are a separate
interaction and must not be forced into this selection component.

Preserve keyboard opening, arrow/Home/End navigation, Escape dismissal,
focus return after selection or Escape, outside-click/blur dismissal, selected
state semantics and disabled behavior. Validate changed behavior through
component tests and inspect the affected callers at desktop and mobile widths.
