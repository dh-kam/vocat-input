/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'Plus Jakarta Sans', 'Outfit', 'sans-serif'],
        mono: ['Monaco', 'Monaco Nerd Font', 'Fira Code', 'monospace'],
      },
      colors: {
        slate: {
          850: '#111827',
          950: '#030712',
        },
        cyan: {
          450: '#00ccff',
        }
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'spin-slow': 'spin 8s linear infinite',
      }
    },
  },
  plugins: [],
}
