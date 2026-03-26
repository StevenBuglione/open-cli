import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import CodeBlock from '@theme/CodeBlock';
import styles from './homepage.module.css';

const snippet = `# Start the hosted runtime once
open-cli-toolbox --config .cli.json --addr 127.0.0.1:8765

# In another shell, inspect the governed catalog
ocli --runtime http://127.0.0.1:8765 --config .cli.json catalog list

# Inspect or execute a tool through the runtime boundary
ocli --runtime http://127.0.0.1:8765 --config .cli.json explain tickets:listTickets`.trim();

const deepLinks = [
  {label: 'Quickstart', href: '/docs/getting-started/quickstart'},
  {label: 'CLI reference', href: '/docs/cli/overview'},
  {label: 'Configuration', href: '/docs/configuration/overview'},
  {label: 'Deployment models', href: '/docs/runtime/deployment-models'},
  {label: 'Security overview', href: '/docs/security/overview'},
  {label: 'Enterprise overview', href: '/docs/enterprise/overview'},
];

export default function FirstRunSnippet(): ReactNode {
  return (
    <section className={styles.firstRunSection} aria-labelledby="first-run-heading">
      <div className="container">
        <div className={styles.sectionHeader}>
          <Heading as="h2" id="first-run-heading">
            Your first run
          </Heading>
          <p>
            Start <code>open-cli-toolbox</code>, then drive it with <code>ocli</code>.
            See the <Link to="/docs/getting-started/quickstart">quickstart</Link> for
            installation and <code>.cli.json</code> setup.
          </p>
        </div>
        <div className={styles.snippetWrapper}>
          <CodeBlock language="bash">{snippet}</CodeBlock>
        </div>
        <nav aria-label="Quick links" className={styles.deepLinkNav}>
          <p className={styles.deepLinkLabel}>Jump to a section</p>
          <ul className={styles.deepLinkList}>
            {deepLinks.map((link) => (
              <li key={link.href}>
                <Link to={link.href} className={styles.deepLinkItem}>
                  {link.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </section>
  );
}
