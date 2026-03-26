import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './homepage.module.css';

type BoundaryRow = {
  concern: string;
  cliEnforces: string;
  runtimeEnforces: string;
};

const rows: BoundaryRow[] = [
  {
    concern: 'Catalog visibility',
    cliEnforces: 'Reads the effective catalog that the runtime built.',
    runtimeEnforces: 'Applies mode filters, agent profiles, and policy rules at build time.',
  },
  {
    concern: 'Auth resolution',
    cliEnforces: 'Passes request context; does not hold secrets.',
    runtimeEnforces: 'Resolves credentials from secret sources per request.',
  },
  {
    concern: 'Execution approval',
    cliEnforces: 'Surfaces approval prompts when the runtime requires them.',
    runtimeEnforces: 'Evaluates approval gates defined in policy before executing.',
  },
  {
    concern: 'Audit logging',
    cliEnforces: 'No independent audit log.',
    runtimeEnforces: 'Appends a structured event for every tool call, approval, and auth resolution.',
  },
  {
    concern: 'Schema validation',
    cliEnforces: 'Can validate request shape against the OpenAPI schema.',
    runtimeEnforces: 'Validates against the normalized effective schema at execution time.',
  },
];

export default function TrustBoundary(): ReactNode {
  return (
    <section
      className={clsx(styles.section, styles.sectionMuted)}
      aria-labelledby="trust-boundary-heading">
      <div className="container">
        <div className={styles.sectionHeader}>
          <Heading as="h2" id="trust-boundary-heading">
            CLI vs runtime: where enforcement lives
          </Heading>
          <p>
            The CLI is a thin command surface. Enforcement — policy, auth, audit — lives in
            the runtime. This split is intentional: the CLI can be replaced or bypassed; the
            hosted runtime holds the invariants.
          </p>
        </div>
        <div className={styles.tableWrapper} role="region" aria-label="Trust boundary table" tabIndex={0}>
          <table className={styles.boundaryTable}>
            <thead>
              <tr>
                <th scope="col">Concern</th>
                <th scope="col">CLI (ocli)</th>
                <th scope="col">Runtime (open-cli-toolbox)</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.concern}>
                  <td>
                    <strong>{row.concern}</strong>
                  </td>
                  <td>{row.cliEnforces}</td>
                  <td>{row.runtimeEnforces}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className={styles.trustNote}>
          Full details in the <Link to="/docs/security/overview">Security overview</Link>{' '}
          and <Link to="/docs/security/policy-and-approval">Policy and approval</Link>.
        </p>
      </div>
    </section>
  );
}
