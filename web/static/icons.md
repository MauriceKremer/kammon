# Icon sprite

`icons.svg` holds the icons as SVG `<symbol>` entries, copied from
[lucide](https://github.com/lucide-icons/lucide) tag `0.577.0`
(commit `0ea8780434e2b4527dc860b18ff94883d080b59c`, ISC licence). Vendored so no CDN is needed.

Use: `<svg class="icon" aria-hidden="true"><use href="/static/icons.svg#sun"></use></svg>`

Icons included: sun, moon, pause, play, activity, arrow-up-right, log-out, shield-check,
lock-keyhole, square-terminal, plus, user-minus, check-check, save, copy.

To add one: fetch `icons/<name>.svg` at the pinned tag, extract the children of the root
element into a `<symbol id="<name>" viewBox="0 0 24 24">`, and update this list.
