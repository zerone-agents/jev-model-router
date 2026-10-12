# Frontend review requirements

Apply [Frontend coding standards](frontend-standards.md) to every UI diff.

For each added or changed dropdown selector, verify:

- It imports the shared `ChoicePicker`; native selects, copied implementations
  and caller-specific visual overrides require a documented exception.
- Options have stable unique values, the label is localized and accessible,
  and controlled selection and disabled states match the workflow.
- Shared component or style changes work in both pagination and Playground;
  check keyboard navigation, focus restoration, dismissal and selected state.
- Long labels and scrollable option lists remain usable at narrow widths.

Report violations under **Standards**, with the affected location and user
impact. Review behavior against the originating request separately under
**Spec**. Cite actual checks performed; green tests alone do not establish
visual consistency.
