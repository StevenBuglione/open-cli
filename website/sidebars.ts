import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  mainSidebar: [
    {
      type: 'category',
      label: 'Getting Started',
      link: {type: 'doc', id: 'getting-started/intro'},
      items: ['getting-started/installation', 'getting-started/quickstart'],
    },
    {
      type: 'category',
      label: 'CLI',
      link: {type: 'doc', id: 'cli/overview'},
      items: ['cli/catalog-and-explain', 'cli/tool-execution', 'cli/workflow-run'],
    },
    {
      type: 'category',
      label: 'Runtime',
      link: {type: 'doc', id: 'runtime/overview'},
      items: ['runtime/http-api', 'runtime/refresh-and-audit', 'runtime/deployment-models'],
    },
    {
      type: 'category',
      label: 'Configuration',
      link: {type: 'doc', id: 'configuration/overview'},
      items: ['configuration/scope-merging', 'configuration/modes-and-profiles', 'configuration/config-schema'],
    },
    {
      type: 'category',
      label: 'Discovery & Catalog',
      link: {type: 'doc', id: 'discovery-catalog/overview'},
      items: [
        'discovery-catalog/api-catalog-discovery',
        'discovery-catalog/service-discovery-and-overlays',
        'discovery-catalog/normalized-tool-catalog',
      ],
    },
    {
      type: 'category',
      label: 'Security',
      link: {type: 'doc', id: 'security/overview'},
      items: ['security/auth-resolution', 'security/policy-and-approval', 'security/secret-sources'],
    },
    {
      type: 'category',
      label: 'Workflows & Guidance',
      link: {type: 'doc', id: 'workflows-guidance/overview'},
      items: [
        'workflows-guidance/skill-manifests',
        'workflows-guidance/arazzo-workflows',
        'workflows-guidance/operator-guidance',
      ],
    },
    {
      type: 'category',
      label: 'Operations',
      link: {type: 'doc', id: 'operations/overview'},
      items: ['operations/audit-logging', 'operations/cache-and-refresh', 'operations/tracing-and-instances'],
    },
    {
      type: 'category',
      label: 'Development',
      link: {type: 'doc', id: 'development/overview'},
      items: ['development/repo-layout', 'development/testing', 'development/extending-the-runtime'],
    },
  ],
};

export default sidebars;
