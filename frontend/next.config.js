/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  
  // Server-side rewrites for API proxy
  async rewrites() {
    // In Docker, use the service name; locally use localhost
    const apiUrl = process.env.API_URL || 'http://api:8080'
    console.log('API URL:', apiUrl)
    return [
      {
        source: '/api/:path*',
        destination: `${apiUrl}/api/:path*`,
      },
    ]
  },
}

module.exports = nextConfig
