import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './homepage.module.css';

export type PersonaPath = {
  id: string;
  title: string;
  audience: string;
  description: string;
  links: {label: string; href: string}[];
};

const paths: PersonaPath[] = [
  {
    id: 'beginner',
    title: 'First run',
    audience: 'End users · agent authors',
    description:
      'You want commands that work. Start with the quickstart, understand the two binaries, then connect ocli to a hosted runtime.',
    links: [
      {label: 'Choose your path', href: '/docs/getting-started/choose-your-path'},
      {label: 'Quickstart', href: '/docs/getting-started/quickstart'},
      {label: 'CLI overview', href: '/docs/cli/overview'},
    ],
  },
  {
    id: 'expert',
    title: 'Runtime depth',
    audience: 'Operators · developers',
    description:
      'You are hosting open-cli-toolbox, hardening a deployment, or wiring up auth, policy, and overlays for shared use.',
    links: [
      {label: 'Deployment models', href: '/docs/runtime/deployment-models'},
      {label: 'Configuration overview', href: '/docs/configuration/overview'},
      {label: 'Security overview', href: '/docs/security/overview'},
    ],
  },
  {
    id: 'enterprise',
    title: 'Enterprise evaluation',
    audience: 'Security reviewers · procurement',
    description:
      'You need a reviewable evidence package: hosted-runtime auth proof, reproducible test artifacts, auditability, and known gaps.',
    links: [
      {label: 'Enterprise overview', href: '/docs/enterprise/overview'},
      {label: 'Adoption checklist', href: '/docs/enterprise/adoption-checklist'},
      {label: 'Fleet validation', href: '/docs/development/fleet-validation'},
    ],
  },
];

export default function PersonaPaths(): ReactNode {
  return (
    <section
      className={clsx(styles.section, styles.sectionMuted)}
      aria-labelledby="persona-paths-heading">
      <div className="container">
        <div className={styles.sectionHeader}>
          <Heading as="h2" id="persona-paths-heading">
            Choose the path that matches why you are here
          </Heading>
          <p>
            Each path links to the first doc that matters for your role — no hunting
            through unrelated sections.
          </p>
        </div>
        <div className={styles.grid3}>
          {paths.map((path) => (
            <article key={path.id} className={styles.personaCard}>
              <header>
                <Heading as="h3" className={styles.personaTitle}>
                  {path.title}
                </Heading>
                <p className={styles.personaAudience}>{path.audience}</p>
              </header>
              <p className={styles.personaDescription}>{path.description}</p>
              <nav aria-label={`${path.title} links`}>
                <ul className={styles.personaLinks}>
                  {path.links.map((link) => (
                    <li key={link.href}>
                      <Link to={link.href}>{link.label}</Link>
                    </li>
                  ))}
                </ul>
              </nav>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
