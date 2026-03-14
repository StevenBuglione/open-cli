import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'oas-cli-go',
  tagline: 'A stateless CLI and local runtime for OpenAPI discovery, policy, and execution.',
  favicon: 'img/favicon.ico',

  url: 'https://stevenbuglione.github.io',
  baseUrl: '/oas-cli-go/',

  organizationName: 'StevenBuglione',
  projectName: 'oas-cli-go',
  trailingSlash: false,

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/StevenBuglione/oas-cli-go/edit/main/website/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/social-card.svg',
    navbar: {
      title: 'oas-cli-go',
      logo: {
        alt: 'oas-cli-go logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'mainSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          href: 'https://github.com/StevenBuglione/oas-cli-go',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Getting Started', to: '/docs/getting-started/intro'},
            {label: 'CLI', to: '/docs/cli/overview'},
            {label: 'Runtime', to: '/docs/runtime/overview'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/StevenBuglione/oas-cli-go'},
            {label: 'Issues', href: 'https://github.com/StevenBuglione/oas-cli-go/issues'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} StevenBuglione. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'json', 'go'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
