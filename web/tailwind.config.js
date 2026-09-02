/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        obsidian: "#0B0D13",
        celluloid: "#121622",
        "celluloid-hover": "#181D2C",
        "celluloid-border": "rgba(255, 255, 255, 0.08)",
        amber: {
          DEFAULT: "#E5A93C",
          bright: "#F5BE58",
          glow: "rgba(229, 169, 60, 0.25)",
        },
        crimson: {
          DEFAULT: "#E11D48",
          glow: "rgba(225, 29, 72, 0.25)",
        },
        silver: {
          DEFAULT: "#F1F3F7",
          dim: "#8B95A5",
          faint: "#4B5565",
        },
        ink: "#0B0D13",
        paper: "#F1F3F7",
        accent: "#E5A93C",
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

