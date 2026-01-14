import type { Metadata } from 'next'
import './globals.css'
import { Providers } from './providers'

export const metadata: Metadata = {
  title: 'Egressor | Data Transfer Intelligence',
  description: 'Detect, explain, and reduce unexpected data transfer in distributed systems',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" className="dark">
      <body className="font-mono bg-flow-bg text-flow-text antialiased">
        <Providers>
          {children}
        </Providers>
      </body>
    </html>
  )
}
