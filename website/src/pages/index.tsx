import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import styles from './index.module.css';

type FeatureItem = {
  title: string;
  description: string;
  href: string;
};

const features: FeatureItem[] = [
  {
    title: 'Get started quickly',
    description:
      'Build the binaries, launch oasclird, and use oascli to inspect the effective catalog in a few commands.',
    href: '/docs/getting-started/quickstart',
  },
  {
    title: 'Learn the CLI surface',
    description:
      'Understand catalog, explain, workflow, and dynamic tool commands without having to reverse-engineer the runtime internals.',
    href: '/docs/cli/overview',
  },
  {
    title: 'Understand the runtime',
    description:
      'Follow how oasclird discovers sources, applies policy, executes tools, refreshes metadata, and records audit events.',
    href: '/docs/runtime/overview',
  },
  {
    title: 'Configure safely',
    description:
      'See how .cli.json scopes merge, how modes and agent profiles filter the catalog, and where schema validation fits.',
    href: '/docs/configuration/overview',
  },
  {
    title: 'Map discovery to tools',
    description:
      'Connect RFC 9727 catalogs, RFC 8631 service discovery, overlays, and normalized tool IDs into one mental model.',
    href: '/docs/discovery-catalog/overview',
  },
  {
    title: 'Operate with guardrails',
    description:
      'Review auth resolution, approval gates, audit logs, refresh flows, and instance isolation before you automate against the runtime.',
    href: '/docs/security/overview',
  },
  {
    title: 'Evaluate enterprise auth',
    description:
      'Start from the Authentik reference proof when you need a concrete, brokered runtime-auth example with real browser login and scoped runtime access.',
    href: '/docs/runtime/authentik-reference',
  },
  {
    title: 'See fleet validation',
    description:
      'Follow the reproducible capability matrix and live proof inventory that the repo now uses to validate local daemon, remote runtime, MCP, and remote API paths.',
    href: '/docs/development/fleet-validation',
  },
];

const keySections = [
  {label: 'Getting Started', href: '/docs/getting-started/intro'},
  {label: 'CLI', href: '/docs/cli/overview'},
  {label: 'Runtime', href: '/docs/runtime/overview'},
  {label: 'Configuration', href: '/docs/configuration/overview'},
  {label: 'Discovery & Catalog', href: '/docs/discovery-catalog/overview'},
  {label: 'Security', href: '/docs/security/overview'},
  {label: 'Workflows & Guidance', href: '/docs/workflows-guidance/overview'},
  {label: 'Operations', href: '/docs/operations/overview'},
  {label: 'Development', href: '/docs/development/overview'},
];

function HomepageHero() {
  const {siteConfig} = useDocusaurusContext();

  return (
    <header className={clsx('hero hero--primary', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className="hero__title">
          {siteConfig.title}
        </Heading>
        <p className={clsx('hero__subtitle', styles.heroLead)}>
          <strong>oascli</strong> is the operator-facing command surface. <strong>oasclird</strong>{' '}
          is the local runtime that handles discovery, normalization, policy, auth, workflow validation,
          and audit logging.
        </p>
        <p className={styles.heroSummary}>
          Together they solve a common problem: turning scattered OpenAPI descriptions into a consistent,
          governed tool interface that humans and agents can actually use.
        </p>
        <div className={styles.buttons}>
          <Link className="button button--primary button--lg" to="/docs/getting-started/quickstart">
            Start with Quickstart
          </Link>
          <Link
            className="button button--secondary button--lg"
            to="/docs/runtime/authentik-reference">
            Evaluate Enterprise Auth
          </Link>
        </div>
      </div>
    </header>
  );
}

function FeatureCard({title, description, href}: FeatureItem) {
  return (
    <article className={styles.card}>
      <Heading as="h3" className={styles.cardTitle}>
        <Link to={href}>{title}</Link>
      </Heading>
      <p className={styles.cardBody}>{description}</p>
      <Link className={styles.cardLink} to={href}>
        Open section →
      </Link>
    </article>
  );
}

export default function Home(): ReactNode {
  return (
    <Layout
      title="Docs home"
      description="Documentation for oascli and oasclird: discovery, policy, configuration, workflows, and operations.">
      <HomepageHero />
      <main>
        <section className={styles.section}>
          <div className="container">
            <div className={styles.sectionHeader}>
              <Heading as="h2">What the two binaries do</Heading>
              <p>
                Use <code>oascli</code> when you need a stable command interface for catalog inspection,
                tool execution, and workflow entry points. Use <code>oasclird</code> when you need the
                runtime that discovers sources, builds the effective catalog, resolves auth, enforces
                policy, and records execution history.
              </p>
            </div>
            <div className={styles.grid}>
              {features.map((feature) => (
                <FeatureCard key={feature.title} {...feature} />
              ))}
            </div>
          </div>
        </section>
        <section className={clsx(styles.section, styles.sectionMuted)}>
          <div className="container">
            <div className={styles.sectionHeader}>
              <Heading as="h2">Start with the section that matches your job</Heading>
              <p>
                Each section groups the current implementation into a stable navigation model so you can
                move from first-run setup to runtime policy, operations, and development details without
                hunting through the source tree.
              </p>
            </div>
            <div className={styles.quickLinks}>
              {keySections.map((section) => (
                <Link key={section.label} className={styles.quickLink} to={section.href}>
                  {section.label}
                </Link>
              ))}
            </div>
          </div>
        </section>
        <section className={styles.section}>
          <div className="container">
            <div className={styles.sectionHeader}>
              <Heading as="h2">Choose the path that matches why you are here</Heading>
              <p>
                New users should start with the quickstart and CLI overview. Operators and enterprise
                evaluators should jump directly to runtime deployment, security, the Authentik reference
                proof, and fleet validation.
              </p>
            </div>
            <div className={styles.quickLinks}>
              <Link className={styles.quickLink} to="/docs/getting-started/quickstart">
                First run
              </Link>
              <Link className={styles.quickLink} to="/docs/cli/overview">
                CLI mental model
              </Link>
              <Link className={styles.quickLink} to="/docs/runtime/deployment-models">
                Runtime deployment
              </Link>
              <Link className={styles.quickLink} to="/docs/runtime/authentik-reference">
                Enterprise auth proof
              </Link>
              <Link className={styles.quickLink} to="/docs/development/fleet-validation">
                Fleet validation
              </Link>
            </div>
          </div>
        </section>
      </main>
    </Layout>
  );
}
