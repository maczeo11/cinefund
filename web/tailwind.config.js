/** @type {import('tailwindcss').Config} */

// Every colour reads a channel variable defined once in src/index.css, so
// Tailwind utilities (bg-amber/15), App.css (var(--accent)) and the film-grade
// backgrounds can never drift apart. Change the palette there.
const c = name => `rgb(var(--c-${name}) / <alpha-value>)`

export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // surfaces, darkest to lightest
        obsidian: c('obsidian'),
        celluloid: c('celluloid'),
        raised: c('raised'),
        amber: {
          DEFAULT: c('amber'),
          bright: c('amber-bright'),
        },
        crimson: {
          DEFAULT: c('crimson'),
        },
        // text: paper > dim > faint, all >= 4.5:1 on every surface
        silver: {
          DEFAULT: c('silver'),
          dim: c('dim'),
          faint: c('faint'),
        },
        ink: c('obsidian'),
        paper: c('silver'),
        accent: c('amber'),
        // The success/"funded" colour. The components say `emerald-*`; this
        // remaps that scale to teal so shadows lean cool against the amber
        // highlights (the teal-and-orange grade) without touching every file.
        emerald: {
          300: '#99F6E4',
          400: c('teal'),
          500: '#14B8A6',
          800: '#115E59',
          950: '#042F2E',
        },
      },
      fontFamily: {
        cinema: ["Cinzel", "serif"],
        display: ["Space Grotesk", "sans-serif"],
        sans: ["Plus Jakarta Sans", "sans-serif"],
        serif: ["Cinzel", "serif"],
      },
    },
  },
  plugins: [],
}
