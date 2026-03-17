/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./templates/**/*.html', './assets/src/**/*.{js,jsx}'],
  theme: {
    fontFamily: {
      'sans': ['Inter', 'system-ui', 'sans-serif'],
      'logo': ['Comfortaa', 'cursive'],
    },
    extend: {
      minWidth: {
        '80': '20rem',
      },
      zIndex: {
        'dropdown':       '10',
        'sticky':         '20',
        'navbar':         '30',
        'modal-backdrop': '40',
        'modal':          '50',
        'toast':          '60',
        'tooltip':        '70',
      },
      colors: {
        w: {
          bg:      '#0a0e1a',
          surface: '#111827',
          card:    '#1a2235',
          pink:    '#e84393',
          pinkL:   '#fd79a8',
          purple:  '#6c5ce7',
          purpleL: '#a29bfe',
          cyan:    '#00cec9',
          text:    '#f1f5f9',
          sub:     '#94a3b8',
          muted:   '#64748b',
          line:    'rgba(255,255,255,0.07)',
        }
      },
    },
  },
}
