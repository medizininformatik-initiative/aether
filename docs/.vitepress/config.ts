import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Aether',
  description: 'Healthcare Data Integration Platform',
  lang: 'en-US',
  base: '/aether/',

  // Favicon configuration
  head: [
    ['link', { rel: 'icon', type: 'image/x-icon', href: '/aether/favicon.ico' }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '32x32', href: '/aether/favicon-32x32.png' }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '48x48', href: '/aether/favicon-48x48.png' }],
    ['link', { rel: 'apple-touch-icon', sizes: '180x180', href: '/aether/apple-touch-icon.png' }]
  ],

  // Theme configuration
  themeConfig: {
    // Logo and site branding
    logo: '/assets/logo.svg',
    siteTitle: 'Aether',

    // Navigation sidebar
    sidebar: {
      '/': [
        {
          text: 'Getting Started',
          collapsed: false,
          items: [
            { text: 'Installation', link: '/getting-started/installation' },
            { text: 'Quick Start', link: '/getting-started/quick-start' },
            { text: 'Configuration', link: '/getting-started/configuration' }
          ]
        },
        {
          text: 'Guides',
          collapsed: false,
          items: [
            { text: 'TORCH Integration', link: '/guides/torch-integration' },
            { text: 'DIMP Pseudonymization', link: '/guides/dimp-pseudonymization' },
            { text: 'Pipeline Steps', link: '/guides/pipeline-steps' }
          ]
        },
        {
          text: 'API Reference',
          collapsed: false,
          items: [
            { text: 'CLI Commands', link: '/api-reference/cli-commands' },
            { text: 'Configuration Reference', link: '/api-reference/config-reference' }
          ]
        },
        {
          text: 'Development',
          collapsed: false,
          items: [
            { text: 'Architecture', link: '/development/architecture' },
            { text: 'Testing', link: '/development/testing' },
            { text: 'Contributing', link: '/development/contributing' },
            { text: 'Coding Guidelines', link: '/development/coding-guidelines' }
          ]
        }
      ]
    },

    // Search configuration
    search: {
      provider: 'local'
    },

    // Footer
    footer: {
      message: 'Healthcare data integration made simple',
      copyright: 'Copyright © 2025 Aether Project'
    },

    // Social links
    socialLinks: [
      { icon: 'github', link: 'https://github.com/medizininformatik-initiative/aether' }
    ]
  },

  // Markdown configuration
  markdown: {
    lineNumbers: true,
    theme: {
      light: 'github-light',
      dark: 'github-dark'
    }
  }
})
