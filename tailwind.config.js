/** @type {import('tailwindcss').Config} */
module.exports = {
  purge: ['./templates/**/*.html', './assets/src/**/*.js'],
  content: [],
  theme: {
    fontFamily: {
      'baskerville': ['Libre Baskerville', 'serif'],
    },
    extend: {
      minWidth: {
        '80': '20rem',
      },
    },
  },
}
