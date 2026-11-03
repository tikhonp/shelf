# Design

Empty until the Claude Design session for phase 1. The session works from [../design-brief.md](../design-brief.md).

What goes here afterwards:

- `screens/MS-*.html`: one exported file per screen.
- `tokens.css`: colors for both themes, type scale, spacing, radii and shadows as CSS variables.
- This README, rewritten with the design's rules: spacing, table behavior, accent color, state colors.

Step 1g builds the Vue app from these files. Once the app exists, its CSS is the source of record; a small visual change goes straight into the app, and a bigger one goes through the canvas first.
