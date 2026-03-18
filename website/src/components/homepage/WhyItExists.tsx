import type {ReactNode} from 'react';
import clsx from 'clsx';
import Heading from '@theme/Heading';
import styles from './homepage.module.css';

type ProblemItem = {
  heading: string;
  body: string;
};

const problems: ProblemItem[] = [
  {
    heading: 'Scattered API descriptions',
    body: 'OpenAPI files live in repos, behind service-discovery endpoints, in registries. No two look the same; tool IDs clash; schemas drift between sources and reality.',
  },
  {
    heading: 'Implicit, unaudited credentials',
    body: 'Auth tokens are pasted into config files or passed as env vars with no per-request resolution, no scope filtering, and no record of who authenticated as what.',
  },
  {
    heading: 'No enforcement layer',
    body: 'Any CLI or agent can call any operation. There is no policy gate, no approval workflow, and no structured record of what executed — until something goes wrong.',
  },
];

export default function WhyItExists(): ReactNode {
  return (
    <section
      className={clsx(styles.section, styles.sectionMuted)}
      aria-labelledby="why-it-exists-heading">
      <div className="container">
        <div className={styles.sectionHeader}>
          <Heading as="h2" id="why-it-exists-heading">
            The problem it solves
          </Heading>
          <p>
            Most teams operate OpenAPI tooling with three unresolved gaps.
          </p>
        </div>
        <div className={styles.whyGrid}>
          {problems.map((p) => (
            <div key={p.heading} className={styles.whyBlock}>
              <h3 className={styles.whyBlockTitle}>{p.heading}</h3>
              <p className={styles.whyBlockBody}>{p.body}</p>
            </div>
          ))}
        </div>
        <p className={styles.whySolution}>
          <strong>oclird</strong> closes all three gaps — discovery, auth
          resolution, policy enforcement, and audit — before any command reaches
          the wire.
        </p>
      </div>
    </section>
  );
}
