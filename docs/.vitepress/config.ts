import { defineConfig } from 'vitepress'

const basePath = process.env.VITE_BASE_PATH || '/aether/'

export default defineConfig({
  title: 'Aether',
  description: 'Healthcare Data Integration Platform',
  lang: 'en-US',
  base: basePath,

  // Favicon configuration
  head: [
    ['link', { rel: 'icon', type: 'image/x-icon', href: `${basePath}favicon.ico` }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '16x16', href: `${basePath}favicon-16x16.png` }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '32x32', href: `${basePath}favicon-32x32.png` }],
    ['link', { rel: 'icon', type: 'image/png', sizes: '96x96', href: `${basePath}favicon-96x96.png` }],
    ['link', { rel: 'apple-touch-icon', sizes: '180x180', href: `${basePath}apple-touch-icon.png` }]
  ],

  // Theme configuration
  themeConfig: {
    // Logo and site branding
    logo: '/assets/logo.svg',
    siteTitle: 'Aether',

    // Top navigation bar
    nav: [
      {
        text: 'GitHub',
        items: [
          {
            text: 'Issues',
            link: 'https://github.com/medizininformatik-initiative/aether/issues'
          },
          {
            text: 'Discussions',
            link: 'https://github.com/medizininformatik-initiative/aether/discussions'
          },
          {
            text: 'Releases',
            link: 'https://github.com/medizininformatik-initiative/aether/releases'
          }
        ]
      }
    ],

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
            { text: 'DIMP', link: '/guides/dimp' },
            {
              text: 'Pipeline Steps',
              link: '/guides/pipeline-steps',
              collapsed: true,
              items: [
                { text: 'TORCH Import', link: '/guides/steps/torch-import' },
                { text: 'Local Import', link: '/guides/steps/local-import' },
                { text: 'HTTP Import', link: '/guides/steps/http-import' },
                { text: 'Validation', link: '/guides/steps/validation' },
                { text: 'DIMP', link: '/guides/steps/dimp' },
                { text: 'Flattening', link: '/guides/steps/flattening' },
                { text: 'Wait', link: '/guides/steps/wait' },
                { text: 'Send', link: '/guides/steps/send' }
              ]
            }
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
