import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './homepage.module.css';

type DeploymentMode = {
  id: string;
  name: string;
  tagline: string;
  description: string;
  tradeoffs: string;
  href: string;
};

const modes: DeploymentMode[] = [
  {
    id: 'embedded',
    name: 'Embedded',
    tagline: 'No daemon required',
    description:
      'The runtime runs in-process for each ocli invocation. Zero setup, no background process, ideal for developer laptops and CI.',
    tradeoffs: 'Cold start on every call; no shared cache across invocations.',
    href: '/docs/runtime/deployment-models',
  },
  {
    id: 'local-daemon',
    name: 'Local daemon',
    tagline: 'Shared, warmed runtime',
    description:
      'A single oclird process persists across CLI calls. Catalog is discovered once, cached, and reused. Supports instance isolation via --instance-id.',
    tradeoffs: 'Requires a running daemon; process management is your responsibility.',
    href: '/docs/runtime/deployment-models',
  },
  {
    id: 'remote-runtime',
    name: 'Remote runtime',
    tagline: 'Centrally hosted',
    description:
      'oclird runs on a shared host. Access is network-controlled with runtime bearer auth. Suitable for teams and fleet deployments.',
    tradeoffs:
      'Network dependency; auth must be configured; trust boundary shifts to the host.',
    href: '/docs/runtime/deployment-models',
  },
];

export default function DeploymentModes(): ReactNode {
  return (
    <section
      className={clsx(styles.section, styles.sectionMuted)}
      aria-labelledby="deployment-modes-heading">
      <div className="container">
        <div className={styles.sectionHeader}>
          <Heading as="h2" id="deployment-modes-heading">
            Deployment modes
          </Heading>
          <p>
            Three topologies — pick the one that matches your operational context.
          </p>
        </div>
        <figure className={styles.deploymentDiagram}>
          <img
            src="/img/deployment-modes.svg"
            alt="Three deployment topologies: Embedded (ocli and runtime in one process), Local daemon (ocli talks to a local oclird over IPC), Remote runtime (ocli reaches oclird over the network with bearer auth)."
            width="680"
            height="210"
            loading="lazy"
          />
          <figcaption className={styles.diagramCaption}>
            Left to right: embedded · local daemon · remote runtime
          </figcaption>
        </figure>
        <div className={styles.grid3}>
          {modes.map((mode) => (
            <article key={mode.id} className={styles.modeCard}>
              <header>
                <Heading as="h3" className={styles.modeTitle}>
                  {mode.name}
                </Heading>
                <p className={styles.modeTagline}>{mode.tagline}</p>
              </header>
              <p>{mode.description}</p>
              <p className={styles.modeTradeoffs}>
                <strong>Trade-off:</strong> {mode.tradeoffs}
              </p>
              <Link to={mode.href} className={styles.cardLink}>
                Deployment models →
              </Link>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
