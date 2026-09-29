# NivaroOS brand

The mark is **"Ni"**: an N whose last stroke is also the stem of an i. The
i's dot is a mint status light, the "server is up" dot.

Every file here, and every logo/icon the web UI, the Android app and GitHub
ship, comes from [`gen.js`](gen.js). Change the geometry or colours there,
never by hand-editing an exported file:

```sh
npm i --no-save --prefix docs/brand opentype.js   # once, for the wordmark outlines
node docs/brand/gen.js          # SVGs (docs/brand + ui/src/assets/img/logo + ui/public)
node docs/brand/gen.js --png    # + every PNG/ICO (needs google-chrome and ImageMagick)
```

## Files

| File | Use |
| --- | --- |
| `nivaroos-icon.svg`, `-1024/-512/-192.png` | App icon: blue tile, white N, mint dot. Default everywhere. |
| `nivaroos-icon-dark.svg` | Icon on an ink tile, for dark UI where a blue tile is too loud. |
| `nivaroos-glyph.svg` | The mark alone, blue with a teal dot, for light backgrounds. |
| `nivaroos-glyph-white.svg` | The mark alone, white with a mint dot, for dark or blue backgrounds. |
| `nivaroos-glyph-mono.svg` | One colour (ink). Source for monochrome uses: Android themed icon, Safari pinned tab. |
| `nivaroos-logo.svg`, `.png` | Lockup (icon + wordmark), ink text, for light backgrounds. |
| `nivaroos-logo-white.svg`, `.png` | Lockup with white text, for dark backgrounds. |
| `nivaroos-wordmark.svg` | Wordmark alone. |
| `social-preview.png` | 1280x640 GitHub social preview (white lockup on ink). |

The wordmark is Geist: "Nivaro" in SemiBold (600), "OS" in Regular (400),
tracking -1.2%, outlined to paths so no font is needed.

## Colours

| Name | Hex | Role |
| --- | --- | --- |
| Blue | `#2563EB` | Tile, glyph on light, primary brand colour, theme colour |
| Mint | `#7DF9C5` | Status dot on blue / dark |
| Teal | `#0EA371` | Status dot on light (mint is too pale on white) |
| Ink | `#0F1115` | Text, dark tile, monochrome |
| White | `#FFFFFF` | N on the tile, text on dark |

## Geometry (256 grid)

- Tile: 256 x 256, corner radius 60.
- N: one path `M80 194 V110 L176 194 V110`, stroke 32, round caps and joins.
- Dot: circle at (176, 62), r 17.
- The mark's bounding box is centred on the tile; its farthest point is about
  98 units from the centre, which is what the adaptive/maskable icons use to
  stay inside their safe zones (Android: the 256 grid spans 80 of 108 dp).
- Lockup: wordmark cap height centred on the tile, 56 units gap.

## Clear space and minimum size

- Keep clear space of at least **a quarter of the tile height** (64 on the
  256 grid) on every side of the icon or lockup. Nothing else (text, edges,
  other logos) goes inside it.
- Minimum sizes: icon 16 px; lockup 20 px tall. Below 24 px use the icon, not
  the lockup.

## Do

- Use the white lockup / white glyph on dark backgrounds and photos, the ink
  lockup / blue glyph on light ones.
- Scale the files as they are, keeping proportions.
- Use the tile on a full-bleed blue square where the platform masks corners
  itself (iOS, Android adaptive, PWA maskable) - `gen.js` already does this.

## Don't

- Recolour the N or the dot, or swap which is mint and which is white.
- Stretch, rotate, outline, add shadows or gradients, or change the stroke
  weight.
- Set "NivaroOS" in another font next to the mark in place of the wordmark.
- Put the blue tile on a blue background of a similar shade.
- Bring back the old three-circle CasaOS cloud mark.
