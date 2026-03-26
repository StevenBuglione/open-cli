import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './homepage.module.css';

export default function Hero(): ReactNode {
  return (
    <header className={clsx('hero hero--primary', styles.heroBanner)} role="banner">
      <div className="container">
        <Heading as="h1" className={clsx('hero__title', styles.heroTitle)}>
          Governed OpenAPI execution,{' '}
          <span className={styles.heroTitleAccent}>end to end.</span>
        </Heading>
        <p className={clsx('hero__subtitle', styles.heroLead)}>
          <strong>open-cli-toolbox</strong> is the hosted runtime that discovers APIs,
          normalizes catalogs, resolves credentials, enforces policy, and writes audit
          events. <strong>ocli</strong> is the thin client that renders commands and sends
          execution requests into that remote boundary.
        </p>
        <div className={styles.heroButtons}>
          <Link
            className="button button--primary button--lg"
            to="/docs/getting-started/quickstart">
            Get started in 5 minutes
          </Link>
          <Link
            className="button button--secondary button--lg"
            to="/docs/enterprise/overview">
            Evaluate enterprise readiness
          </Link>
        </div>
      </div>
    </header>
  );
}
