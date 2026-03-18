import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './homepage.module.css';

type Step = {
  number: number;
  label: string;
  detail: string;
  href: string;
};

const steps: Step[] = [
  {
    number: 1,
    label: 'Point at your sources',
    detail:
      'Local OpenAPI files, URLs, RFC 9727 catalogs, or RFC 8631 service-discovery endpoints. The runtime finds and fetches them automatically.',
    href: '/docs/discovery-catalog/overview',
  },
  {
    number: 2,
    label: 'Runtime governs the catalog',
    detail:
      'oclird deduplicates and normalises tool IDs, applies OpenAPI overlays, evaluates policy rules, and resolves per-request credentials — before any execution occurs.',
    href: '/docs/runtime/deployment-models',
  },
  {
    number: 3,
    label: 'Execute with confidence',
    detail:
      'Use ocli commands or the built-in MCP server. Every tool call, approval gate decision, and auth event is appended to a structured audit log.',
    href: '/docs/cli/overview',
  },
];

export default function HowItWorks(): ReactNode {
  return (
    <section className={styles.section} aria-labelledby="how-it-works-heading">
      <div className="container">
        <div className={styles.sectionHeader}>
          <Heading as="h2" id="how-it-works-heading">
            How it works
          </Heading>
          <p>
            Three stages from raw OpenAPI to a governed, auditable command surface.
          </p>
        </div>
        <div className={styles.howItWorksLayout}>
          <ol className={styles.stepList} role="list">
            {steps.map((step) => (
              <li key={step.number} className={styles.stepItem}>
                <span className={styles.stepNumber} aria-hidden="true">
                  {step.number}
                </span>
                <div className={styles.stepContent}>
                  <strong className={styles.stepLabel}>
                    <Link to={step.href}>{step.label}</Link>
                  </strong>
                  <p className={styles.stepDetail}>{step.detail}</p>
                </div>
              </li>
            ))}
          </ol>
          <figure className={styles.howItWorksDiagram}>
            <img
              src="/img/runtime-flow.svg"
              alt="Flow: OpenAPI sources feed into oclird (discover, normalise, policy, auth, audit), which exposes the governed catalog to ocli commands and the MCP server."
              width="420"
              height="200"
              loading="lazy"
            />
            <figcaption className={styles.diagramCaption}>
              Sources → governed catalog → command surface
            </figcaption>
          </figure>
        </div>
      </div>
    </section>
  );
}
