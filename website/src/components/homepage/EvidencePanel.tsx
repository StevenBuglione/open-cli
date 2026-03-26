import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './homepage.module.css';

type EvidenceItem = {
  id: string;
  title: string;
  description: string;
  href: string;
  label: string;
};

const evidence: EvidenceItem[] = [
  {
    id: 'authentik',
    title: 'Authentik reference proof',
    description:
      'A worked, reproducible example of runtime bearer auth brokered through Authentik. Documents client-credentials flow, scope filtering, and Entra federation. Not a vendor endorsement — a concrete test fixture.',
    href: '/docs/runtime/authentik-reference',
    label: 'View auth proof',
  },
  {
    id: 'fleet-validation',
    title: 'Fleet validation matrix',
    description:
      'CI-reproducible capability matrix covering hosted runtime auth, MCP, and remote API paths. Each cell maps to a concrete test artifact or known gap — no unverified claims.',
    href: '/docs/development/fleet-validation',
    label: 'View fleet matrix',
  },
  {
    id: 'spec-conformance',
    title: 'Spec & conformance',
    description:
      'OpenAPI overlay processing, RFC 9727 catalog support, and RFC 8631 service discovery are each tracked against the published spec. Conformance notes surface known gaps.',
    href: '/docs/discovery-catalog/overview',
    label: 'View discovery docs',
  },
  {
    id: 'audit',
    title: 'Audit surfaces',
    description:
      'The runtime appends a structured audit log entry for every tool execution, approval gate decision, and auth resolution. Log schema and retention are documented.',
    href: '/docs/security/overview',
    label: 'View security docs',
  },
];

export default function EvidencePanel(): ReactNode {
  return (
    <section className={styles.section} aria-labelledby="evidence-panel-heading">
      <div className="container">
        <div className={styles.sectionHeader}>
          <Heading as="h2" id="evidence-panel-heading">
            Evidence for enterprise evaluation
          </Heading>
          <p>
            Reproducible proof artifacts — not marketing claims. Each item links to a
            concrete doc or test fixture.
          </p>
        </div>
        <div className={styles.grid2}>
          {evidence.map((item) => (
            <article key={item.id} className={styles.evidenceCard}>
              <Heading as="h3" className={styles.evidenceTitle}>
                {item.title}
              </Heading>
              <p>{item.description}</p>
              <Link to={item.href} className={styles.cardLink}>
                {item.label} →
              </Link>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
