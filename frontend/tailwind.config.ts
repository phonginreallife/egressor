import type { Config } from 'tailwindcss'

const config: Config = {
  content: [
    './src/pages/**/*.{js,ts,jsx,tsx,mdx}',
    './src/components/**/*.{js,ts,jsx,tsx,mdx}',
    './src/app/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    extend: {
      colors: {
        // Dark theme inspired by terminal aesthetics
        'flow': {
          bg: '#0a0e14',
          surface: '#121820',
          border: '#1f2937',
          text: '#e5e7eb',
          muted: '#6b7280',
          accent: '#10b981', // Emerald for data flowing
          warning: '#f59e0b',
          danger: '#ef4444',
          info: '#3b82f6',
          egress: '#f97316', // Orange for outbound
          ingress: '#06b6d4', // Cyan for inbound
          internal: '#8b5cf6', // Purple for internal
        },
      },
      fontFamily: {
        sans: ['JetBrains Mono', 'Fira Code', 'monospace'],
        mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'flow': 'flow 2s linear infinite',
      },
      keyframes: {
        flow: {
          '0%': { strokeDashoffset: '100' },
          '100%': { strokeDashoffset: '0' },
        },
      },
    },
  },
  plugins: [],
}
export default config
