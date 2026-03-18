import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import CodeBlock from '@theme/CodeBlock';
import styles from './homepage.module.css';

const snippet = `# Embedded mode — no daemon needed for a first run
ocli --embedded --config .cli.json catalog list

# Inspect a tool's schema and usage guidance
ocli --embedded --config .cli.json explain tickets:listTickets

# Execute a tool — dynamic commands are shaped by your OpenAPI spec
ocli --embedded --config .cli.json helpdesk tickets list-tickets --status open`.trim();

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
            Three commands from install to executed tool — no daemon required.
            See the{' '}
            <Link to="/docs/getting-started/quickstart">quickstart</Link> for
            installation and <code>.cli.json</code> config setup.
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
