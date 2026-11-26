---
name: Musaarr
description: Request one hi-fi album in a single gesture and read its state, hard-bop sleeve style, until it is in Plex.
colors:
  blue: "#1d5ce0"
  on-blue: "#ffffff"
  ochre: "#f0b323"
  on-ochre: "#111111"
  vermilion: "#ef5a3c"
  on-vermilion: "#111111"
  green: "#34b36b"
  on-green: "#111111"
  ground: "#0b0b0c"
  raised: "#161618"
  line: "#2a2a2e"
  ink: "#f2f2ee"
  ink-soft: "#a3a3a8"
  track: "#202024"
  blue-ink: "#6f9bff"
  ochre-ink: "#e8b42a"
  vermilion-ink: "#ff8a70"
  green-ink: "#4cc982"
  ground-light: "#f7f7f5"
  raised-light: "#ffffff"
  line-light: "#dcdcd6"
  ink-light: "#101012"
  ink-soft-light: "#56565c"
  track-light: "#e4e4df"
  blue-ink-light: "#1a52c9"
  ochre-ink-light: "#8a6100"
  vermilion-ink-light: "#b8321a"
  green-ink-light: "#17773f"
typography:
  display:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "clamp(52px, 15vw, 96px)"
    fontWeight: 900
    lineHeight: 0.92
    letterSpacing: "-0.04em"
  display-name:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "clamp(44px, 12vw, 96px)"
    fontWeight: 900
    lineHeight: 0.92
    letterSpacing: "-0.04em"
  page-title:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "clamp(36px, 10vw, 56px)"
    fontWeight: 900
    lineHeight: 0.95
    letterSpacing: "-0.04em"
  state-big:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "clamp(22px, 6.4vw, 36px)"
    fontWeight: 900
    lineHeight: 1
    letterSpacing: "-0.03em"
  wordmark:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "30px"
    fontWeight: 900
    lineHeight: 1
    letterSpacing: "-0.04em"
  headline:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 800
    lineHeight: 1.1
    letterSpacing: "-0.02em"
  state:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 800
    lineHeight: 1.12
    letterSpacing: "-0.01em"
  title:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 700
    lineHeight: 1.25
  body:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.4
  body-small:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.4
  label:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 700
    lineHeight: 1.1
  tab:
    fontFamily: "Libre Franklin, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 600
rounded:
  none: "0px"
spacing:
  gutter: "16px"
  tabbar: "58px"
  tap: "44px"
  grid-gap-mobile: "28px 14px"
  grid-gap: "32px 20px"
  grid-gap-wide: "36px 24px"
  container: "1240px"
components:
  button-blue:
    backgroundColor: "{colors.blue}"
    textColor: "{colors.on-blue}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0 14px"
    height: "44px"
  button-line:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0 14px"
    height: "44px"
  segment:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "0 12px"
    height: "44px"
  segment-pressed:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.ground}"
  search-field:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "0 14px 0 46px"
    height: "52px"
  tab:
    textColor: "{colors.ink-soft}"
    typography: "{typography.tab}"
    height: "{spacing.tabbar}"
  tab-selected:
    textColor: "{colors.ink}"
  band-track:
    backgroundColor: "{colors.track}"
    textColor: "{colors.ink}"
    typography: "{typography.state}"
    rounded: "{rounded.none}"
    padding: "9px 12px"
    height: "44px"
  band-active:
    backgroundColor: "{colors.blue}"
    textColor: "{colors.on-blue}"
  band-waiting:
    backgroundColor: "{colors.ochre}"
    textColor: "{colors.on-ochre}"
  band-failed:
    backgroundColor: "{colors.vermilion}"
    textColor: "{colors.on-vermilion}"
  band-in-plex:
    backgroundColor: "{colors.green}"
    textColor: "{colors.on-green}"
  band-big:
    typography: "{typography.state-big}"
    padding: "12px 14px"
    height: "60px"
  toast:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.ground}"
    rounded: "{rounded.none}"
    padding: "14px 16px"
    width: "min(100vw - 32px, 440px)"
---

# Design System: Musaarr

## Overview

**Creative North Star: "The Hard-Bop Sleeve"**

Musaarr is printed, not rendered. Every surface borrows from the hard-bop jazz sleeves of the late 1950s: an ink-black or paper-white ground, one heavy grotesque set tight, square artwork with no radius, and four flat spot inks that carry meaning and nothing else. The signature is the status band: a request's state is a title block, its word set in Black on the state's spot ink, filling left to right with download progress, the text inverting exactly where the ink passes. It replaces the category's rounded cards with small coloured status chips.

Density is that of a record shelf: real covers in a 2 to 5 column grid, one bold prompt, very little chrome. Album covers are content and stay true; artist portraits are printed as a blue duotone so a person never reads as an album. Motion is an orchestrated ink wipe plus a blink for a live search in the lists; the v3 adds three physical worlds where things move as records do: the room (the Bibliothèque), the shop (the search home, racks of crates since v4) and the 3D stage (one record, printed as a halftone).

Provenance: the hard-bop direction was built as the recommended option, then chosen by the owner on 22/11/2026. The tokens below record the shipped build.

**Key Characteristics:**
- Libre Franklin only, Black 900 for display and state words, 400/600/700 for reading.
- Ink black (#0b0b0c) and paper white (#f7f7f5) grounds, never cream.
- Four spot inks, identical in both themes: blue active, ochre waiting/check, vermilion failure, green in Plex.
- Zero radius everywhere except the record itself, which is round; 2px ink rules for controls, 1px hairlines for structure.
- Flat print: one blurred shadow in the system, on the floating toast; the room's wood casts a hard 7px block, printed, never blurred.
- French UI copy, strict CSP, AA contrast in both themes.

## Colors

Two neutral grounds and four flat spot inks; the inks never change between themes, the readable "-ink" text tints do.

### Primary
- **Ink Blue** (blue): the active ink. Fills the bands for Demandé, Recherche en cours, Téléchargement, Import en cours and Importé; the Demander and Ouvrir dans Plex buttons; the tab badge; the desktop selected-tab underline; the duotone portrait ground; text selection; native accent-color. White text on it (on-blue).
- **Signal Blue text** (blue-ink, blue-ink-light): blue as text and strokes on the ground: links like Tout voir and Recherche (back), the selected tab icon stroke, focus rings, caret.

### Secondary
- **Sleeve Ochre** (ochre): the waiting ink, for En attente d'une source and À vérifier. Near-black text on it (on-ochre).
- **Ochre text** (ochre-ink, ochre-ink-light): the per-request note line.

### Tertiary
- **Vermilion** (vermilion): the failure ink, Échec du téléchargement only. Near-black text on it.
- **Vermilion text** (vermilion-ink, vermilion-ink-light): error notices.
- **Plex Green** (green): the arrival ink, Dans Plex only. Near-black text on it.
- **Green text** (green-ink, green-ink-light): the arrival tint for text, declared in both themes but not yet used by the build; use it before inventing another green.

### Neutral
- **Ink Ground / Paper Ground** (ground, ground-light): page, sticky search bar, tab bar, status-bar shield.
- **Raised** (raised, raised-light): search field fill, empty sleeve, skeleton bars.
- **Track** (track, track-light): the unfilled part of a status band.
- **Line** (line, line-light): row dividers, tab-bar top rule, scrollbar thumb.
- **Ink** (ink, ink-light): text, 2px control borders, pressed segment fill, toast fill.
- **Soft Ink** (ink-soft, ink-soft-light): secondary text, placeholders, idle tabs, search icon.

Dark is the `:root` default (`color-scheme: dark`); light overrides ground, raised, line, ink, ink-soft, track, the four -ink tints and the shadow under `prefers-color-scheme: light`. The spot inks and their on-ink colours are declared once.

### Contrast pairs (WCAG AA, measured)
- on-blue on blue 5.74; on-ochre on ochre 10.05; on-vermilion on vermilion 5.57; on-green on green 7.02.
- Dark: ink on ground 17.5, ink-soft on ground 7.8, ink on track 14.5; blue-ink 7.3, ochre-ink 10.3, vermilion-ink 8.6, green-ink 9.4 on ground.
- Light: ink on ground 17.7, ink-soft on ground 6.8, ink on track 14.9; blue-ink 6.3, ochre-ink 5.2, vermilion-ink 5.6, green-ink 5.2 on ground.
- The raw blue spot is non-text only on the dark ground (3.43, enough for the 3px tab underline and portrait ground, not for text): use blue-ink for blue text.

### Named Rules
**The Spot Ink Rule.** The four spot inks mean state and nothing else. Blue is also the one action colour (Demander). No gradients, no tints of a spot ink as decoration, no fifth hue.

**The Paired Ink Rule.** Every spot ink is used with its on-ink colour; text on a spot ink keeps at least 4.5:1. Blue text on a ground is always the -ink tint, never the spot.

**The True Cover Rule.** Album covers are shown exactly as delivered: no tint, filter, overlay, blend or radius. A missing cover shows the text Pas de pochette on raised; a cover is never invented or generated.

## Typography

**Display Font:** Libre Franklin (variable 100 to 900, self-hosted woff2, `font-display: swap`; fallback system-ui, sans-serif)
**Body Font:** Libre Franklin
**Label/Mono Font:** none; figures use `tabular-nums` where they tick (band percentages, segment counts).

**Character:** One grotesque doing the whole sleeve: Black 900 at tight tracking for headlines and state words, regular and semibold for reading. The contrast comes from weight and size, never from a second family.

### Hierarchy
- **Display** (900, clamp(52px, 15vw, 96px), 0.92, -0.04em): the search prompt Quel artiste ?
- **Display name** (900, clamp(44px, 12vw, 96px), 0.92, -0.04em): the artist page name.
- **Page title** (900, clamp(36px, 10vw, 56px), 0.95, -0.04em): Bibliothèque.
- **State big** (900, clamp(22px, 6.4vw, 36px), 1, -0.03em): the state word in a big band.
- **Wordmark** (900, 30px, 36px from 900px wide, -0.04em): lowercase "musaarr".
- **Headline** (800, 22px, 1.1, -0.02em): section titles Artistes, Albums, Dernières demandes; the empty-state line (800, 28px) is its larger sibling.
- **Title** (700, 16px card / 17px row, 1.2 to 1.25): album and artist names; two-line clamp on cards.
- **Body** (400, 16px, 1.4): default; prompts and empty text at 17px in ink-soft, max 34ch; notices and explanations max 60ch.
- **Body small** (400, 14px): card and row sub-lines, explanations; 13px for artist disambiguation and Pas de pochette.
- **State** (800, 15px, 1.12, -0.01em): the state word in a standard band.
- **Label** (700, 15px, 1.1): buttons and segments; 600 for links (Tout voir, Recherche) and toast text.
- **Tab** (600, 12px under the icon on mobile, 15px inline from 900px).

### Named Rules
**The One Family Rule.** Libre Franklin is the only face. Hierarchy is weight (400/600/700/800/900) and size, never a second family or italics.

**The Black Is Reserved Rule.** Weight 900 is for display lines, the wordmark and state words in bands. Body copy never goes above 700.

## Layout

Mobile first. Content sits in a centred column (max 1240px) with a 16px gutter. On mobile a fixed bottom tab bar (58px plus the safe-area inset) holds Rechercher and Bibliothèque, and the body pads its bottom to clear it; from 900px the tab bar moves into the header, top right, and the body pad drops to 48px. The header is 56px tall (76px from 900px). The wordmark is a link home: it empties the search and shows the search page.

The search field is sticky under the safe-area top inset, full bleed to the gutter, on the ground colour. A fixed shield the height of `env(safe-area-inset-top)` keeps scrolled content out from under the iOS status bar.

Breakpoints: 600px, 900px, 1140px. The album grid runs 2 columns (gaps 28px/14px), 3 from 600px (32px/20px), 4 from 900px, 5 from 1140px (36px/24px). The artist strip is a horizontal scroll-snap row of 132px columns on mobile; from 900px it becomes a grid on the same columns as the albums, each artist a line (80px portrait, name, two-line disambiguation) so the first albums stay above the fold.

Requests are a list of rows divided by 1px lines: on mobile a 76px sleeve beside the title, with the band, explanation, note and actions spanning the full width below. From 900px every row is three columns (96px sleeve, 180 to 280px names, the rest) and every band sits in the third column so the states read down the page. The compact row (Dernières demandes on the search home) uses a 56px sleeve and a standard band.

Below 900px the incontournables run as a rail: one row of sleeves at 44% of the width (30% from 600px), scroll-snapped, bleeding to the screen edge. From 900px they fall back to the album grid.

The album page puts the sleeve beside the title: 38% of the width on mobile with the action under both, a 280px sleeve column from 700px (340px from 900px) with the title and action stacked in the second column. Its tracker list, the artist's others and the trackers line stop at 960px, like the source list.

Every interactive target is at least 44px tall. Spacing is set per element rather than from a numeric scale; the recurring steps are 12/14/16px inside components and 22 to 28px between blocks.

## Elevation & Depth

Flat print. Depth comes from the ground/raised/track tonal steps and from the ink itself, not from shadows. Sleeves carry a 1px inset hairline (ink at 12% via color-mix) so a white sleeve keeps an edge on paper. The only shadow belongs to the toast, the one element that floats over content.

### Shadow Vocabulary
- **Toast lift, dark** (`box-shadow: 0 1px 2px rgb(0 0 0 / 50%), 0 10px 24px rgb(0 0 0 / 40%)`): the toast only.
- **Toast lift, light** (`box-shadow: 0 1px 2px rgb(16 16 18 / 14%), 0 10px 20px rgb(16 16 18 / 10%)`): the toast only.

### Named Rules
**The Flat Print Rule.** Nothing on the page casts a blurred shadow except the toast. Sleeves, bands, buttons and rows are flat. In the room, a plank or a crate casts a hard block of `--cast` 7px straight down onto the wall (no blur, no spread): a second printed tone, like an offset plate, not a rendered light.

## Shapes

Zero radius, everywhere: sleeves, portraits, buttons, segments, the search field, bands, the badge, the toast. The one round thing is a record: the vinyl that slides out of a sleeve on a shelf, the turntable's platter, and the 3D record. Squares are the silhouette (`aspect-ratio: 1` for sleeves and portraits). Controls are drawn with 2px solid ink rules; structure with 1px line hairlines. The status band's mark is an 8px square, not a dot. Icons are 24px (20px on desktop tabs and the search field) inline SVG strokes, 2 to 2.4px, round caps, `currentColor`.

## Components

### Buttons
Printed blocks, confident and square.
- **Shape:** square corners (0px), min height 44px, 0 14px padding, 2px border slot.
- **Blue (primary):** blue fill, white label, 700 15px. Full width under a card (Demander); auto width elsewhere (Chercher un album, Ouvrir dans Plex).
- **Line (secondary):** transparent with a 2px ink border and ink label (Réessayer, Relancer la recherche, Ouvrir Plexamp).
- **Quiet:** no border, soft-ink 600 label underlined 3px below, for the step nobody should take by accident (Retirer). Its first tap arms it for 5 s: « Confirmer le retrait », 700, vermilion-ink label and 2px border; no dialog, since requesting the album again undoes it.
- **Hover (hover-capable pointers only):** blue darkens to color-mix(blue 86%, black); line gets a 9% ink wash.
- **Active:** scale(.97) over .15s on the ease-out curve. **Disabled:** 50% opacity, no transform.
- **Text links:** Tout voir and the back control Recherche (with a chevron) are 600 blue-ink text, 44px tall, turning ink on hover.

### Segments
The discography filter (Albums, EP, Singles, Autres, each with its count in 400 tabular figures). A run of square 2px ink-bordered buttons that overlap their borders by 2px into one strip; the pressed one is filled ink with ground text (`aria-pressed`). Scrolls horizontally on narrow screens; the focused segment lifts above its neighbours so the ring is whole.

Under it the order strip, the same segments at 40px and 14px after a soft-ink « Trier » (hidden from screen readers; the group is labelled « Ordre des disques »): « Les plus écoutés » (the default, from ListenBrainz listens) and « Par date ». The choice is kept on the device. Without listens the strip hides and the discography goes by date.

### Tabs
Rechercher and Bibliothèque (three spines, one leaning), a real `tablist`; the last tab is restored on the next visit (« Mes demandes », its name until 1.7, opens Bibliothèque). Mobile: fixed bottom bar, icon over a 12px 600 label, idle soft ink, selected ink with the icon stroked blue-ink at 2.4. Desktop (900px+): inline, icon beside a 15px label, selected marked by a 3px blue inset underline. The badge counting active requests is a square blue block, white 800 11px, pinned to the icon on mobile and inline on desktop.

### Search field
52px, 2px ink border, raised fill, 500 17px text, a soft-ink search icon inset at 15px. Focus turns the border and a 2px outline blue-ink. Placeholder Artiste ou album in soft ink. Results put first the artist the query is, or the artist of an album found that the query names (« Middle of Nowhere – Kacey Musgraves » leads to Kacey Musgraves), then Lidarr's guesses in its order, those whose name holds the query and more words last (« kanye west »: Ye before the tribute bands); every album card links to its page.

### Cover sleeve
The album as an object: a square, true-colour cover filling a raised square with the inset hairline. No cover yields the centred 13px soft-ink text Pas de pochette. Under it, title (700, two-line clamp), artist, then year, type and listens (« 3,3 M d'écoutes », « 162 k écoutes ») in soft ink, then the action: the Demander button, or the album's band once it has a state. Loading cards breathe (opacity to .45 over 1.4s).

The sleeve and the title link to the album's page (no underline on the sleeve, the title underlines on hover). On the artist page a quality line sits between the meta and the action, always 26px tall so the grid does not move when the trackers answer: a breathing track-coloured bar while they search, then the best release's quality as a chip (solid when lossless, « FLAC 24/96 ») and « N sorties », or « Aucune source » in 600 soft ink. While the search runs the bar is hidden from screen readers; the trackers line speaks for it.

### Duotone portrait
Artist images printed as a blue duotone: the grayscale, contrast 1.15 photo multiplied onto the blue spot ink. Without an image, the artist's initial in 900 white on blue (56px in the strip, 36px on desktop lines, 44 to 176px heads on the artist page). Square, no radius. This is the only place an image is altered, and it is never an album cover.

### Status band (signature)
The state word set on its spot ink, as a title block.
- **Structure:** a track-coloured block with the label in ink; over it an absolutely positioned fill in the state's ink carrying its own copy of the label in the on-ink colour. The fill is clipped with `clip-path: inset(0 calc(100% - var(--p)) 0 0)`, so the text inverts exactly where the ink passes. The copy in the fill is `aria-hidden`.
- **States and inks:** Demandé, Recherche en cours, Téléchargement, Import en cours, Importé on blue; En attente d'une source and À vérifier on ochre; Échec du téléchargement on vermilion; Dans Plex on green.
- **Fill:** every state is 100% except Téléchargement, which fills to its progress. When a request row is redrawn in the same state, the fill animates forward from the last drawn percentage (.8s ease-out). A downloading band is a `progressbar` with value text such as "42 %, environ 12 min".
- **Mark:** an 8px square in currentColor precedes Demandé, Recherche en cours and Import en cours; for Recherche en cours it blinks.
- **Standard (card and compact row):** min 44px, 9px 12px padding, state type 800 15px; stays on one line on a card, figures omitted.
- **Big (En route rows):** min 60px (76px from 900px), state-big type, and an aside after the word in 600 14 to 15px tabular figures: "42 % · ≈ 12 min".
- **Card variant as link:** on a card already in Plex the band itself is the link to Plex, underlined on hover.

### Requests list

« En route », at the top of the Bibliothèque, puts first what needs the user (À vérifier, Échec), then what moves, then En attente d'une source, newest first within each. A request that reaches Plex leaves it: the shelf under it shows the album. The block hides when nothing is on its way.

The `details` fold (« Autres sorties sur tes trackers », an edition) is a summary in the block-title type (800 22px) with its count in soft ink and a 2.5px soft-ink chevron that turns on open (reduced motion: no turn animation).

### Bibliothèque (the shelf)
Under « En route », « Dans Plex »: every album Plex holds as cover sleeves on the album grid, since the shelf is the record collection itself. The page sub-line counts them with what is on its way (« 1 404 albums dans Plex · 2 en cours · 1 à vérifier »).
- **Controls:** the search field (sticky over the whole grid, placeholder « Titre, artiste ou genre », filters as you type), then segments « Albums 1 404 » and « Artistes 212 », then the filters: native selects drawn as 40px segments with a 2px chevron (« Derniers ajoutés », « Derniers écoutés », « Par artiste », « Par année », « Par titre »; « Décennies » → « Années 1970 »; « Genres »), and the « Jamais écoutés » toggle. A filter in use prints solid like a pressed segment. A soft-ink status line counts what is kept (« 4 albums sur 1 404 ») with a blue-ink « Tout effacer ».
- **Cards:** the cover sleeve, title, artist, then the year and what the order is about: « ajouté le 23 nov. », « écouté le 12 nov. » or « jamais écouté », else the first genre. Drawn 60 at a time as the page nears its end. The sleeve and the title open the album page; while Musaarr finds the album in MusicBrainz the sleeve breathes. Not found, the card says « Introuvable dans MusicBrainz. » in 600 soft ink and offers a full-width line button « Ouvrir dans Plex », which takes the focus.
- **Artists:** a ruled list (1 column, 2 from 600px, 3 from 900px, 4 from 1140px) of 68px rows: a 52px sleeve of one of the artist's covers, the name in 700 and « 6 albums » in soft ink. A row narrows the grid to that artist under a blue-ink back link « Tous les artistes » and the name in 800 28px; back returns to the row, where the list was scrolled.
- **Album page from the shelf:** its back link reads « Bibliothèque » and returns to the same card at the same scroll.
- **States:** Plex not configured or not answering, a notice with Réessayer (the last shelf seen stays under it, « La liste date de ta dernière visite. »); an empty Plex, « Rien dans Plex pour l'instant. » with « Chercher un album ».

Every request whose source is not already on its way ends its actions with the quiet Retirer. Once retired, the row leaves, focus moves to the page title, and the toast says « … est retiré. Lidarr ne le cherche plus. »

### La pièce (the Bibliothèque as a room)
Above the full grid, the Bibliothèque is a room the user decorates: a wall, shelves of covers standing face out on planks, one object per shelf.
- **Shelves:** « Sur la platine » (the last album played, beside a turntable whose platter turns), « Ma vitrine » (up to 24 records the user pins from an album page with « Mettre en vitrine »), « Les plus écoutés » (numbered), « Écoutés dernièrement », « Arrivés récemment », « Jamais écoutés », « À redécouvrir » (drawn again each day), then one shelf per type Plex gives (Albums, Singles et EP, Live, Compilations, Bandes originales, Remix). 20 covers per shelf, scrolling sideways with snap, with ‹ › arrows (44px, greyed at an end, gone when the shelf fits) on a fine pointer and a mouse drag; each shelf has « Fouiller » (its crate) and the type shelves « Tout voir » (the grid filtered).
- **A record on a shelf:** the cover (124px, 144px from 600px, 160px from 900px) on a 16px plank: a lighter 5px top, the front face with a halftone grain, a 2px darker edge, the 7px cast. Under the pointer or the focus, the vinyl slides up 28% out of its sleeve (.32s ease-out); as the shelf scrolls, the covers near its ends turn a few degrees.
- **Décorer:** a panel of swatches: the wood (Chêne, Noyer, Laqué noir, Blanc, Acier), the wall (Uni, Pois, Lattes, Brique: a pattern masked at 8% of the ink), an object per shelf (Platine, Plante, Lampe, Enceinte, Casque, Cactus, or none: flat SVG drawings), which shelves show and their order. Saved on the NAS (`GET`/`PUT /api/room`, a status line « Enregistré sur le NAS. ») so the iPhone, the iPad and the computer show the same room; « Revenir à la pièce d'origine » keeps the vitrine.

**The Room Rule.** Inside the room, materials (woods, clay, leaves, brass) are printed as flat tones with a halftone grain, never photographic. The spot inks keep their meaning (the turntable's label is the one blue, as on the 3D record), and covers stay true.

### Le bac (the crate)
The Bibliothèque's gesture for digging: a CSS 3D crate of records, opened by « Fouiller » on a shelf or « Au hasard ». The crate takes the whole tab, under a back link and its title.
- **Digging:** the front record leans toward you as it is pulled (finger, wheel, arrow keys, the Précédent and Suivant buttons) and falls forward to show the next; the tops of the records behind rise above it, each 3.5% higher and 40px deeper. A touched record lifts (scale 1.03) before its page opens. Only the records around the front one exist in the page, so a crate of 3,000 stays fluid. The position (« 12 sur 230 », « fond du bac » at the end), the name and the meta are announced.
- **Tools:** native selects drawn as segments; on a phone they share one row the thumb pushes sideways, so the crate stays in view.
- **The box:** the crate's front plank in the room's wood, with the same cast.
- **Under the front record:** its actions (the band, or Demander) and « Ouvrir l'album ».

### La boutique en 3D (since v5, 25/11/2026)
The shop as a room you walk into, full screen on `#disquaire`, always dark, whatever the theme: a record shop at night, never a real chain's name, logo or colours.
- **The front:** on the search home, the room's picture (`shop3d/vitrine.jpg`, 1280×640, object-fit cover, a dark gradient at its foot), the count of rayons and records (« 24 rayons · 40 442 disques ») in brass 600 13px caps, « Entrer dans la boutique » (blue) and « Parcourir les bacs sans la 3D » (underlined text).
- **The room:** a Paris record shop at night, warm tungsten light. Herringbone parquet in satin varnish, walls of warm painted plaster over a bottle-green dado (skirting, rail, panels, cornice), dark beams, a night street behind the green shop front. In a big shop, arcades of basket arches part the rooms one behind the other. The lamps change from room to room: brass bars of opal globes over the lines of bins in one, green enamel billiard shades in the next; prismatic pendants over the counter, tulip sconces along the walls and on the piers, a floor lamp in the listening corner. Their light is baked into the floor and the walls (the floor darkens under and at the foot of the bins), and the few real lights follow you. Each unit has a hanging board in forest green #16241d with a brass rule and the rayon's name in cream Libre Franklin 900 caps. Oak cube shelves along the walls are packed with sleeve spines, under a ledge of covers faced out; between the sconces, posters without words but a genre and a year; on the back wall, two neons (« Disques » pink, « Hi-Fi » blue). At the back stand the counter (green panels, walnut top, brass foot rail), the till, a turntable at 33⅓ between an amplifier with lit VU meters and two speakers on stands, a lounge chair on a rug, and the shop's board with its name, fitted to its language.
- **A unit:** a painted base 78cm high, sage green with raised panels on the aisle side, a walnut rim and a dark plinth, shaded darker toward the floor; crates of light oak 60×36cm with sloped sides and a 11cm front lip; a kraft card at the back of each crate with its letters in Black; a kraft divider with the artist's name before each artist with several records. Records lean back 11°, those seen lean 49° toward you, the one in front stands, lifted 3.5cm. An owned record carries a small green sticker.
- **Over the room:** the title, the hint, then boards for Sortir, Au hasard, « Cacher ceux que j'ai » and one per rayon (forest green, 1px edge, brass on hover and focus). Digging, a panel at the foot shows « Jazz · bac A–E · 3 sur 28 » in brass caps, the title in Black clamp 20 to 30px, artist · year · style, tags « Pressage japonais » (vermilion) and « Chez toi » (green), then « Ouvrir la page de l'album » (blue) and « Revenir dans l'allée ».
- **Motion:** the camera eases toward its target (rate 5.5/s), records turn at 16/s, the pulled sleeve rises 20cm before the album page opens (.38s). Reduced motion moves the camera and the records at once.

### Le disquaire en racks (the shop without 3D, on the search home)
Under « Quel artiste ? », a record shop's floor in racks, never a real chain's name, logo or colours. Since v4 (25/11/2026), from mock-up D « Le rack de boutique ». Since v5 it is the shop without WebGL 2, or when « Parcourir les bacs sans la 3D » was chosen.
- **Rayons:** one rack per genre (Jazz, Soul et R&B, Funk et disco, Hip-hop, Rock, Pop, Électro, Chanson, Classique, Reggae et Afro, Latin et Brésil, Blues, Folk et country, Metal et punk, Bandes originales, Divers), stacked. A bar of rayons sticks under the search field: 40px buttons, 2px line border, the name in 700 and the count in 400 soft tabular figures; the rack crossing the upper third of the screen prints solid ink. Above it, « Cacher ceux que j'ai déjà » (a 20px native checkbox, kept per device) and « Au hasard » in the block head.
- **A rack:** its name in Black 900 (clamp 28 to 40px, -0.035em) with « 182 disques » in soft 400; then a row of crate labels (800 14px soft, the one at the left edge ink with a 3px blue underline) and, on a fine pointer, two 44px square arrows. The crates scroll sideways in one row (thin visible scrollbar, snap by proximity, a mouse drag slides it), 18 to 30px apart.
- **A crate:** 232 to 284px wide (74vw on a phone), records 84% of it. The records stand in a well cut at the lip, with room above for the dividers' tabs: the front one whole but its foot, the 8 behind each 7px higher and 30px deeper, a step darker (brightness −7.5% each). A record flipped forward turns about its foot to −104° and goes under the lip: nothing that moves ever crosses the plank, its label or the caption. The flip takes .46s on the ease-out curve; reduced motion moves without it.
- **The lip:** a 60px plank of photographed plywood, the shop's own material where the room prints its woods flat (Poly Haven CC0, `web/rack/bois.webp`), a 5px lighter top edge, a 2px dark foot and the room's hard 7px cast; on it a paper sticker with the letters in Black 24px (« A–B », or « He » when one letter fills several crates) and « 12 disques » in 600 12px.
- **Dividers:** a kraft card (ambientCG Paper 006 CC0, `web/rack/carton.webp`) the size of a record, the artist in Black and « 12 disques » (with MusicBrainz's sort name for a name in another script) on the card, and a 30px tab sticking up at a staggered place, the artist in 800 13px, cut with « … ». Every divider's tab shows, however deep.
- **Opening:** the first time the racks show, a small crate takes their place while the first screen's covers come (6 s at most): three sleeves (blue, ochre, vermilion) drop into it one after the other on the ease-out curve, over « Le disquaire déballe ses bacs… ». Then those crates come in 45 ms apart (opacity and 14px, .3s); reduced motion keeps a still crate and a plain fade. A rack coming near loads all its crates; a late cover fades in (.3s) over its printed sleeve.
- **Covers:** a Japanese pressing's front when the Cover Art Archive has one (often with its obi), else the usual cover; without a picture, a printed sleeve on a spot ink chosen by the title, the title in Black and the artist in 700 capitals.
- **Caption:** under the crate, three lines always reserved: the title (800 17px, two lines), « Artiste · année · Chez toi » (soft 14px, « Chez toi » in green ink, a request's state otherwise) and « 3 sur 12 » (« fond du bac » at the end; « Intercalaire » and « 12 disques derrière » on a divider).
- **Gestures:** the wheel over a crate flips one record per notch, but a wheel already scrolling the page keeps scrolling it, and so does one past the first or last record. A mouse drag down or up flips; a finger flips from the tops of the records (their `touch-action: pan-x`) and scrolls the page from a cover. ↑ ↓ flip, Page suivante and Page précédente go to the next divider, ← → to the next crate, Début and Fin to the ends, Entrée opens. A tab or a divider in front leads to the artist's first record; a record behind comes to the front; the front record opens its album page, whose back link reads « Disquaire » and returns focus to the crate. The position is announced once the flicking stops.
- **The stock:** the site's most listened albums on ListenBrainz and the albums of the user's artists, rebuilt each day on the NAS; covers come through Musaarr (Cover Art Archive), so the CSP stays the same. While it fills, a soft-ink line says « Le disquaire se remplit… Reviens dans quelques minutes. »; with everything hidden, « Tu as déjà tout ce qu'il y a ici. ».

### La scène 3D (the stage)
One record: black vinyl with grooves that catch a fixed light, five flat track rings, a blue label printed with « musaarr » and « HI-FI · 33 TOURS ». The scene is rendered by three.js and printed through a 45° halftone screen (4.5px cells at home, 5px in the opening): light ink on the ink ground, dark ink on paper, with a floor of fine dots so the black vinyl keeps its shape on black.
- **Opening:** at the first visit only, over everything (the rest of the page inert): the record slides out of a blue sleeve, the sleeve leaves, the record turns up to 33 rpm and leans back, a blue wave runs through its grooves, and the wordmark prints under it with a clip-path wipe. 3.7s; a tap, a click, any key or « Passer l'ouverture » ends it. Remembered on the device; « Revoir l'ouverture » at the end of the home plays it again.
- **At home:** the record turns slowly (.32 rad/s) behind « Quel artiste ? » at the top right: half off the screen on a phone, whole at the right of a wide stage, under the bins. It leans with the mouse, or with the phone's tilt on Android. A mask fades it out over the text column; opacity .6 on ink, .5 on paper; .14 once results cover it.
- **Searching:** the record speeds up to 33 rpm and a blue wave runs out through the grooves; it slows down when the results arrive.
- **Demander or Télécharger:** a small record leaves the button and flies on an arc to the Bibliothèque tab (.76s, cubic-bezier(.77, 0, .175, 1), turning), then the tab's icon pulses once.
- **An arrival in Plex:** an album that reached Plex since the last visit sweeps a green glint once round the record (up to three), and its band in the lists wipes in again.
- **Rules:** three.js r187 is vendored in `web/vendor/` (MIT), loaded as a module after the first paint, never blocking. No 3D and no opening under `prefers-reduced-motion: reduce`, Save-Data or without WebGL 2; the app is whole without it. Pixel ratio capped at 1.5; 30 frames a second while idle; the loop stops when the stage is off screen, behind results or in a hidden tab. A lost WebGL context removes the stage.

### Trackers on the artist page
Opening an artist runs one tracker search under its name, since that gesture is the user asking. Under the order strip a soft-ink line (`role="status"`) says where it is: « Recherche de « Ye » sur tes trackers… » after the blinking 8px blue-ink square, then « 42 sorties sous « Ye » sur tes trackers, pour 9 de ses disques. ». When nothing lands under a disc, the artist's other Latin or Cyrillic names are offered as segment buttons « Chercher sous Kanye West », one search per tap; when the tapped button disappears, focus moves to the line. A failed search turns the line 600 vermilion-ink with a « Réessayer » line button.

Releases that belong to no disc (discographies, box sets, loose titles) wait under the grid in a folded `details` like Dans Plex: « Autres sorties sur tes trackers » and its count, a soft-ink sentence saying what they are, then source rows.

### Album page
Opened from a card, from « Choisir une source » on a request, or from `#artiste/<id>/album/<id>`. A back link carrying the artist's name, then the head: the sleeve, the title in display type (900, clamp 26 to 64px, .95), year, type and listens in soft ink, and the card's action (Demander or the band). Under Demander, a soft-ink note says what it takes: the best lossless release on the trackers, 24-bit first, upgraded to 24-bit if one appears later. When the request waits for a source, the note says instead that the release downloaded here is attached to it, and the toast confirms « Envoyé et rattaché à ta demande de … ». Then « Sur tes trackers »: opening the page is the gesture, so it runs a precise search « Artiste Album » at once (under the name the artist page found releases under, if it did), best quality first, with a status line counting them. When no release carries every word searched, the artist's other Latin or Cyrillic names are offered as segment buttons « Chercher sous Kanye West », one search per tap; focus moves to the heading when the tapped button disappears. Opening an album does not run the artist's search; the artist page runs it when it opens.

Under the head, the sleeve's back: when Plex holds the album, a 600 15px line after an 8px green square (the arrival ink, its only use outside a band) gives the user's version, « Dans ta bibliothèque : FLAC · 24 bits · 96 kHz · édition Japon · 2006 · SHM-CD · 22 pistes ». When the trackers hold a clearly better release of this album, a 15px line under it, « Mieux que ta version : » and the quality in 800, its tracker and sources in soft ink, beside a blue « Remplacer ta version »; once sent, a soft-ink note says musictag will put it in place of the old one. Then « Pistes » (« Tes pistes » when owned) and « Éditions », stacked on a phone and side by side from 900px. A tracklist is rows divided by 1px lines, 40px tall: the number in 600 soft ink tabular figures, the title, the duration in soft ink tabular figures; « Disque 2 » heads each disc after the first (800 15px soft ink). On a phone a tracklist longer than 8 shows 6 rows and a line button « Voir les 13 pistes », since the sources wait under it. Each edition is a folded `details` between hairlines: its name in 700 16px (« Japon · 2007 · CD · bonus track · 14 pistes »), then in soft ink its pressings and what it adds (« 3 pressages · en plus : Good Night »), and the same 2.5px chevron as Dans Plex; open, its label line and its tracklist, the tracks the main edition lacks marked « en plus » by a 1px line-bordered 12px 700 tag. When the metadata server fails, a notice says « Pistes et éditions indisponibles » with Réessayer, and the sources still load.

### Free tracker search
Under a search's results, after a 1px rule, a soft-ink line says the catalogue lacks bootlegs, discographies and tracker compilations, and a line button « Chercher « q » sur tes trackers » opens the free search (`#trackers`): a back link, the page title « Sur tes trackers », the field with its blue « Chercher » button on the same line (52px both), a status line and the source rows. It runs only on Enter or the button, never while typing.

### Incontournables
On the search home, before Dernières demandes: « Les incontournables de tes artistes », a soft-ink sentence (« Leurs albums les plus écoutés sur ListenBrainz, qui manquent dans Plex. »), then up to 12 cover sleeves: the most-listened studio albums of the artists the user already holds, each artist's best first, then each one's second. A requested album leaves the list. The block hides until the daily pass has an answer.

### Source row
One release per row, divided by 1px lines like the requests list: the full title (600 15px, wraps anywhere), a soft-ink meta line (tracker · size in Mo or Go with a decimal comma · « N sources » · age « 3 j », « 2 mois », « 4 ans »), then quality chips, an optional ochre-ink warning (« musictag ne lit pas ce format. » for ISO and DSD) and the blue « Télécharger » button (Envoi…, then Envoyé and disabled). From 900px the button moves to a right column, centred on the row, and the list stops at 960px.
- **Quality chips:** square 1px line-bordered labels, 700 13px, 26px tall: format, « 24 bits », « 96 kHz », « Pistes » or « Image + cue », flags (Discographie, Vinyle, MQA, Multicanal), « Avec perte » and « Format inconnu ». A lossless format chip is printed solid (ink fill, ground text), the one fact to read first. Chips are words, never spot inks: the Spot Ink Rule still holds.
- **Loose answers:** trackers also return releases that do not hold every word searched; they come after the others, under a soft-ink line « Ne contiennent pas tous les mots cherchés » (800 17px).
- **Edition from the name:** a soft-ink 14px line « D'après le nom : édition japonaise, SHM-CD, pressage 2012. » when the name announces one.
- **Which edition:** on an album's page, above that line, a 600 14px line names the album's edition a release is: « Édition probable : Japon · 2013 · 2 CD · 7 pistes. », named after the pressing the name points to (the edition filter's segment keeps the edition's own name); « Édition à confirmer : « Voir le contenu » lit ses pistes. » in 400 soft ink when nothing does; « Un autre disque : Late Registration. » for the artist's other records. Reading the files replaces it (« Édition …, lue dans le .torrent. »).
- **Groups and edition filter:** on an album's page its releases come first, then a split « Autres sorties de Ye », then « Autres résultats » or « Ne contiennent pas tous les mots cherchés ». Above the list, once two editions are found, small segments (40px, wrapping their own words on a phone): « Toutes les éditions 24 », one per edition in the page's order with its count, « À confirmer » last; pressed, a segment shows that edition's releases only, the other groups stay.
- **Voir le contenu:** a line button beside Télécharger (stacked under it in the desktop column). It reads the .torrent's file list, nothing downloads; the button gives way to a 600 15px verdict that takes the focus and is announced (`role="status"`): « C'est l'édition États-Unis · 2007 · CD · 13 pistes. », « 16 pistes : la plus proche est l'édition Japon… En plus : … », « Image + cue : les pistes sont dans le .cue, que Musaarr ne lit pas. », then a folded « 15 fichiers » list in 14px tabular figures, non-audio files in soft ink. A failure prints the verdict in vermilion-ink and gives the button back.
- **No seeders:** the title and chips turn soft ink (the format chip loses its fill; fading by opacity failed AA) and the meta reads « Aucune source »; the button stays readable, since such a release can still start later.

### Offline banner
When Musaarr cannot be reached (Tailscale off, NAS down), a band under the header between two 2px vermilion-ink rules says « Musaarr est injoignable. Vérifie que Tailscale est connecté. » in 600 15px vermilion-ink, with a line button « Réessayer ». The page itself still opens from the service worker's copy, and the last known requests stay readable under it.

### Toast
A square ink slab with ground text (600 15px), max 440px, centred above the tab bar (28px from the bottom on desktop), the system's only shadow. Slides up 10px and fades in; hides after 5s. Carries request and network errors. `role="status"`.

### Notice
Inline status under the search or on the artist page: a soft-ink paragraph (max 60ch), optionally followed by a line button Réessayer. As an error it turns vermilion-ink and 600. Example copy: « Rien trouvé pour « … ». Essaie le nom de l'artiste seul, ou le titre exact de l'album. », « Impossible de joindre Musaarr. Vérifie que Tailscale est connecté, puis réessaie. »

### Motion
- **The ink wipe:** on Demander, the button is replaced by a Demandé band whose fill wipes in from the left (`clip-path` from `inset(0 100% 0 0)`, .65s, cubic-bezier(.16, 1, .3, 1)). This is the one orchestrated moment.
- **Fill advance:** band progress transitions its clip-path over .8s on the same curve.
- **Searching blink:** the Recherche en cours mark fades to .2 opacity and back over 1.4s, infinite.
- **Small feedback:** button press scale .97 (.15s), toast fade .2s and slide .3s, skeleton breathe 1.4s.
- **Physical motion (v3):** records move as records do: the vinyl slides out of its sleeve on a shelf (.32s), the crate's front record leans and falls (.42s ease-out, following the finger while dragged), a touched record lifts, a bin's covers rise, the stage's record turns, speeds up, flies to the tab. All use the strong ease-out cubic-bezier(.23, 1, .32, 1) or, for movement across the screen, cubic-bezier(.77, 0, .175, 1).
- **Reduced motion:** `prefers-reduced-motion: reduce` removes every animation and transition and the 3D stage; bands appear at their final fill, the crate steps without leaning.

### Browser surfaces
- `color-scheme` dark by default, light under the system preference; `theme-color` #0b0b0c dark and #f7f7f5 light.
- Selection is blue with white text; `accent-color` blue; caret blue-ink; scrollbar line on ground.
- Focus: a 2px blue-ink outline offset 2px on every `:focus-visible`.
- Tap highlight removed; text-size-adjust locked; `viewport-fit=cover` with safe-area insets on body, tab bar and toast.
- App icon: a blue square with a white Libre Franklin Black lowercase "m" (icon.svg, rendered to apple-touch-icon.png, icon-192.png, icon-512.png, maskable); the manifest uses the ink ground as background and theme colour; `lang` fr.

## Do's and Don'ts

### Do:
- **Do** set every state word on its spot ink with its paired on-ink colour, through the status band, never as a pill or chip.
- **Do** keep the four spot inks identical across themes and switch only the grounds, lines, inks and -ink text tints.
- **Do** use blue-ink, not the blue spot, for blue text and focus rings.
- **Do** keep covers square, true-colour and unaltered; show Pas de pochette when one is missing.
- **Do** print artist portraits as the blue duotone, or the initial on blue.
- **Do** keep all styling in app.css and all script in app.js (plus stage.js, the 3D module, and the vendored three.js): the CSP is `default-src 'self'; script-src 'self'; style-src 'self'` with images from 'self' and the Lidarr image hosts only. Dynamic values such as the band's `--p` are set through the CSSOM (`style.setProperty`), never an inline `style` attribute or `<style>` block.
- **Do** write UI copy in French, with a narrow no-break space before ? and % (Quel artiste ?, 42 %), and « » quotes.
- **Do** keep 44px minimum targets and the 2px blue-ink focus ring.

### Don't:
- **Don't** round any corner.
- **Don't** add shadows to sleeves, bands, buttons or rows; the toast is the only lifted element.
- **Don't** tint, filter, overlay or invent an album cover.
- **Don't** use a cream or warm off-white ground; paper is #f7f7f5.
- **Don't** add a second typeface or use weight 900 for running text.
- **Don't** add motion beyond the ink wipe, the fill advance, the searching blink, the small feedback and the physical motion listed; nothing animates under reduced motion.
- **Don't** put the 3D record under running text without its mask, or let it tint a cover.
- **Don't** use a real record shop's name, logo or colours for the disquaire.
- **Don't** use a spot ink decoratively, as a gradient, or for a meaning other than its state.
