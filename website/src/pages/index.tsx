import type {ReactNode} from 'react';
import Layout from '@theme/Layout';
import Hero from '../components/homepage/Hero';
import WhyItExists from '../components/homepage/WhyItExists';
import HowItWorks from '../components/homepage/HowItWorks';
import PersonaPaths from '../components/homepage/PersonaPaths';
import CapabilityStrip from '../components/homepage/CapabilityStrip';
import DeploymentModes from '../components/homepage/DeploymentModes';
import TrustBoundary from '../components/homepage/TrustBoundary';
import EvidencePanel from '../components/homepage/EvidencePanel';
import FirstRunSnippet from '../components/homepage/FirstRunSnippet';

export default function Home(): ReactNode {
  return (
    <Layout
      title="ocli &amp; open-cli-toolbox"
      description="Governed OpenAPI execution with a remote-only CLI and hosted runtime. Documentation for ocli and open-cli-toolbox.">
      <Hero />
      <main>
        <WhyItExists />
        <HowItWorks />
        <PersonaPaths />
        <CapabilityStrip />
        <DeploymentModes />
        <TrustBoundary />
        <EvidencePanel />
        <FirstRunSnippet />
      </main>
    </Layout>
  );
}
