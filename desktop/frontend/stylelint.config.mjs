// Guards the Fluent 2 design tokens adopted across the desktop UI: type, weight,
// corner radius and spacing must come from the shared scales, not ad-hoc values.
// Intentional exceptions carry a `stylelint-disable-next-line` comment with a reason.

/** Fluent 2 spacing ramp (XXS 2 … XXXL 32) plus larger multiples of 4. */
const SPACING_RAMP = [0, 1, 2, 4, 6, 8, 10, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48, 56, 64, 72, 80, 96];

// A one- or two-digit px value that is not on the ramp; calc()/clamp()/min()/max()
// expressions are left alone because they derive from tokens.
const offRampSpacing = new RegExp(
  String.raw`^(?!.*(?:calc|clamp|min|max)\().*(?<![\w.])(?!(?:${SPACING_RAMP.join("|")})px\b)\d{1,2}(?:\.\d+)?px`,
);

export default {
  rules: {
    "declaration-property-value-disallowed-list": [
      {
        "font-size": [/^\d+(?:\.\d+)?px$/],
        "font-weight": [/^(?:\d+|bold|bolder|lighter|normal)$/],
        // Fluent var(--lineHeight*) for UI text, var(--hm-line-height-reading|display) otherwise.
        "line-height": [/^(?!var\(|1$|normal$|inherit$)/],
        "/^border(?:-(?:top|bottom|start|end)-(?:left|right|start|end))?-radius$/": [/^(?!.*calc\().*\d+px/],
        "/^(?:padding|margin)(?:-(?:top|right|bottom|left|inline|block)(?:-(?:start|end))?)?$/": [offRampSpacing],
        "/^(?:gap|row-gap|column-gap)$/": [offRampSpacing],
        // Literal colors belong in custom properties (token definitions), never in declarations.
        "/^(?!--)/": [/#[0-9a-f]{3,8}\b/i, /rgba?\(\s*\d/i, /hsla?\(\s*\d/i],
      },
      {
        message: (property, value) =>
          `"${property}: ${value}" bypasses the design tokens. Use the Fluent token (var(--fontSize*), var(--fontWeight*), var(--lineHeight*), var(--borderRadius*)), var(--hm-line-height-reading|display), a color token, or a value on the spacing ramp ${SPACING_RAMP.join("/")}px.`,
      },
    ],
  },
  overrides: [
    {
      // Theme token files define the palette itself.
      files: ["src/theme/*.tokens.css"],
      rules: { "declaration-property-value-disallowed-list": null },
    },
  ],
};
