import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'open-cli',
  tagline: 'A stateless CLI and local runtime for OpenAPI discovery, policy, and execution.',
  favicon: 'img/favicon.ico',

  url: 'https://open-cli.dev',
  baseUrl: '/',

  organizationName: 'StevenBuglione',
  projectName: 'open-cli',
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
          editUrl: 'https://github.com/StevenBuglione/open-cli/edit/main/website/',
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
      title: 'open-cli',
      logo: {
        alt: 'open-cli logo',
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
          type: 'dropdown',
          label: 'Get Started',
          position: 'left',
          items: [
            {type: 'doc', docId: 'getting-started/choose-your-path', label: 'Choose Your Path'},
            {type: 'doc', docId: 'getting-started/installation', label: 'Installation'},
            {type: 'doc', docId: 'getting-started/quickstart', label: 'Quickstart'},
            {type: 'doc', docId: 'cli/overview', label: 'CLI Overview'},
          ],
        },
        {
          type: 'dropdown',
          label: 'Runtime & Security',
          position: 'left',
          items: [
            {type: 'doc', docId: 'runtime/overview', label: 'Runtime'},
            {type: 'doc', docId: 'runtime/deployment-models', label: 'Deployment Models'},
            {type: 'doc', docId: 'security/overview', label: 'Security'},
            {type: 'doc', docId: 'configuration/overview', label: 'Configuration'},
            {type: 'doc', docId: 'discovery-catalog/overview', label: 'Discovery & Catalog'},
            {type: 'doc', docId: 'operations/overview', label: 'Operations'},
          ],
        },
        {
          type: 'dropdown',
          label: 'Enterprise',
          position: 'left',
          items: [
            {type: 'doc', docId: 'enterprise/overview', label: 'Enterprise Overview'},
            {type: 'doc', docId: 'enterprise/adoption-checklist', label: 'Adoption Checklist'},
            {type: 'doc', docId: 'runtime/enterprise-readiness', label: 'Readiness Assessment'},
            {type: 'doc', docId: 'runtime/authentik-reference', label: 'Auth Reference Proof'},
            {type: 'doc', docId: 'development/fleet-validation', label: 'Fleet Validation'},
          ],
        },
        {
          type: 'dropdown',
          label: 'Development',
          position: 'left',
          items: [
            {type: 'doc', docId: 'development/overview', label: 'Development Overview'},
            {type: 'doc', docId: 'development/repo-layout', label: 'Repo Layout'},
            {type: 'doc', docId: 'development/testing', label: 'Testing'},
            {type: 'doc', docId: 'development/extending-the-runtime', label: 'Extending the Runtime'},
          ],
        },
        {
          href: 'https://github.com/StevenBuglione/open-cli',
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
            {label: 'Docs Hub', to: '/docs/intro'},
            {label: 'Getting Started', to: '/docs/getting-started/intro'},
            {label: 'CLI', to: '/docs/cli/overview'},
            {label: 'Runtime', to: '/docs/runtime/overview'},
            {label: 'Enterprise', to: '/docs/enterprise/overview'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/StevenBuglione/open-cli'},
            {label: 'Issues', href: 'https://github.com/StevenBuglione/open-cli/issues'},
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
