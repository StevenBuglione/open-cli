import type {ReactNode} from 'react';
import Layout from '@theme/Layout';
import Hero from '../components/homepage/Hero';
import HowItWorks from '../components/homepage/HowItWorks';
import CapabilityStrip from '../components/homepage/CapabilityStrip';
import PersonaPaths from '../components/homepage/PersonaPaths';
import DeploymentModes from '../components/homepage/DeploymentModes';
import TrustBoundary from '../components/homepage/TrustBoundary';
import EvidencePanel from '../components/homepage/EvidencePanel';

export default function Home(): ReactNode {
  return (
    <Layout
      title="Docs home"
      description="Documentation for oascli and oasclird: discovery, policy, configuration, workflows, and operations.">
      <Hero />
      <main>
        <HowItWorks />
        <CapabilityStrip />
        <PersonaPaths />
        <DeploymentModes />
        <TrustBoundary />
        <EvidencePanel />
      </main>
    </Layout>
  );
}
