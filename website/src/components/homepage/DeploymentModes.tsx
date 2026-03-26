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
    id: 'localhost',
    name: 'Loopback-hosted runtime',
    tagline: 'Fastest way to evaluate',
    description:
      'Run open-cli-toolbox on the same machine and point ocli at http://127.0.0.1:8765. It is still the remote runtime contract, just hosted locally for development.',
    tradeoffs: 'Single-user footprint; you manage process lifecycle and runtime URL wiring.',
    href: '/docs/runtime/deployment-models',
  },
  {
    id: 'shared-runtime',
    name: 'Shared team runtime',
    tagline: 'One hosted control plane',
    description:
      'Host open-cli-toolbox on shared infrastructure so teams, agents, and automation all consume the same governed catalog, policy, cache, and audit boundary.',
    tradeoffs: 'Requires network reachability planning, shared config ownership, and runtime auth.',
    href: '/docs/runtime/deployment-models',
  },
  {
    id: 'enterprise-runtime',
    name: 'Brokered enterprise runtime',
    tagline: 'Security boundary first',
    description:
      'Put open-cli-toolbox behind IdP-issued bearer auth, reverse proxies, and network controls. This is the supported production model for enterprise access review.',
    tradeoffs:
      'You own external perimeter controls, token lifecycle policy, and runtime hosting.',
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
            Hosted runtime topologies
          </Heading>
          <p>
            One supported model — a hosted runtime — with deployment shapes that scale
            from local evaluation to enterprise hosting.
          </p>
        </div>
        <figure className={styles.deploymentDiagram}>
          <img
            src="/img/deployment-modes.svg"
            alt="Three hosted-runtime topologies: loopback-hosted open-cli-toolbox for evaluation, shared team hosting, and brokered enterprise hosting with auth and network controls."
            width="680"
            height="210"
            loading="lazy"
          />
          <figcaption className={styles.diagramCaption}>
            Left to right: local host · shared runtime · brokered enterprise
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
