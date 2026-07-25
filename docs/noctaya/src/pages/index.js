import React from 'react';
import Head from '@docusaurus/Head';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';
import styles from './index.module.css';

const capabilities = [
  {
    number: '01',
    title: 'Scale to zero by design',
    description:
      'Release accelerators when traffic disappears. A lightweight gateway keeps the endpoint available and signals KEDA when the next request arrives.',
  },
  {
    number: '02',
    title: 'Survive the cold path',
    description:
      'Bound admission, preserve activation demand, emit streaming heartbeats, wait for model readiness, and drain in-flight requests safely.',
  },
  {
    number: '03',
    title: 'Compose the stack you have',
    description:
      'Run existing vLLM images across NVIDIA and Ascend, use vendor device plugins, and opt into Volcano or observability without making them core dependencies.',
  },
];

const boundaries = [
  ['You declare', 'Model, runtime, resources, cache, scaling, endpoint'],
  ['Noctaya owns', 'Workload lifecycle, cold activation, admission, drain'],
  ['Your stack owns', 'Inference kernels, device plugins, schedulers, monitoring'],
];

function ArrowIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true">
      <path d="M4 10h11M11 5l5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.8" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true">
      <path d="m4 10 4 4 8-9" fill="none" stroke="currentColor" strokeWidth="2" />
    </svg>
  );
}

function Home() {
  return (
    <Layout
      title="Scale-to-zero LLM serving"
      description="A minimal, composable LLM serving control plane for private Kubernetes clusters."
    >
      <Head>
        <meta property="og:title" content="Noctaya — Scale-to-zero LLM serving" />
        <meta
          property="og:description"
          content="Release idle accelerators without giving up a stable, cold-start-aware inference endpoint."
        />
      </Head>

      <main className={styles.homepage}>
        <section className={styles.hero}>
          <div className={styles.heroGlow} />
          <div className={styles.heroGrid}>
            <div className={styles.heroCopy}>
              <div className={styles.eyebrow}>
                <span className={styles.pulse} />
                Kubernetes-native · scale-to-zero
              </div>
              <h1>
                Serve the long tail.
                <span>Release the accelerator.</span>
              </h1>
              <p className={styles.heroLead}>
                Noctaya is a minimal control plane for bursty LLM workloads on private
                Kubernetes clusters—declarative model lifecycle, cold-start-aware traffic, and
                reusable runtime profiles without a fleet-scale platform.
              </p>
              <div className={styles.heroActions}>
                <Link className={styles.primaryAction} to="/docs/started">
                  Get started
                  <ArrowIcon />
                </Link>
                <Link className={styles.secondaryAction} to="/docs/architecture">
                  Explore the architecture
                </Link>
              </div>
              <div className={styles.heroProof}>
                <span><CheckIcon /> OpenAI-compatible endpoint</span>
                <span><CheckIcon /> NVIDIA and Ascend</span>
                <span><CheckIcon /> Apache-2.0</span>
              </div>
            </div>

            <div className={styles.terminalWrap} aria-label="Noctaya scale-to-zero lifecycle">
              <div className={styles.terminal}>
                <div className={styles.terminalTop}>
                  <div className={styles.windowDots}><i /><i /><i /></div>
                  <span>cluster / ai</span>
                  <span className={styles.liveBadge}>live</span>
                </div>
                <div className={styles.terminalBody}>
                  <p><span className={styles.prompt}>$</span> kubectl get llmservice</p>
                  <div className={styles.tableHead}>
                    <span>NAME</span><span>PHASE</span><span>REPLICAS</span>
                  </div>
                  <div className={styles.tableRow}>
                    <span>qwen-longtail</span>
                    <span className={styles.zero}>ScaledToZero</span>
                    <span>0</span>
                  </div>
                  <div className={styles.eventLine}>
                    <span>request admitted</span>
                    <strong>gateway → KEDA</strong>
                  </div>
                  <div className={styles.scaleTrack}>
                    <div className={styles.scaleNode}>0</div>
                    <div className={styles.scaleLine}><span /></div>
                    <div className={styles.scaleNodeActive}>1</div>
                    <div className={styles.scaleLine}><span /></div>
                    <div className={styles.scaleNode}>N</div>
                    <div className={styles.scaleLine}><span /></div>
                    <div className={styles.scaleNode}>0</div>
                  </div>
                  <div className={styles.eventLog}>
                    <p><time>00:00</time><span>demand preserved</span></p>
                    <p><time>00:04</time><span>backend scheduled</span></p>
                    <p><time>00:31</time><span><i className={styles.readyDot} />model ready · streaming</span></p>
                  </div>
                </div>
              </div>
              <div className={styles.terminalCaption}>
                <span>One stable endpoint</span>
                <span>Accelerators only when needed</span>
              </div>
            </div>
          </div>
        </section>

        <section className={styles.validationBand} aria-label="Validated ecosystem">
          <div className={styles.sectionShell}>
            <span className={styles.bandLabel}>Validated across</span>
            <div className={styles.bandItems}>
              <span>NVIDIA A10</span>
              <span>NVIDIA A100</span>
              <span>Atlas 300I Duo</span>
              <span>Ascend 910B3</span>
              <span>KEDA</span>
              <span>Volcano</span>
            </div>
          </div>
        </section>

        <section className={styles.capabilitySection}>
          <div className={styles.sectionShell}>
            <div className={styles.sectionIntro}>
              <p className={styles.kicker}>Why Noctaya</p>
              <h2>Small control plane. Complete cold-start lifecycle.</h2>
              <p>
                Scaling a Deployment to zero is the easy part. Noctaya concentrates on what
                happens when demand returns.
              </p>
            </div>
            <div className={styles.capabilityGrid}>
              {capabilities.map((capability) => (
                <article className={styles.capabilityCard} key={capability.number}>
                  <span className={styles.cardNumber}>{capability.number}</span>
                  <h3>{capability.title}</h3>
                  <p>{capability.description}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className={styles.architectureSection}>
          <div className={`${styles.sectionShell} ${styles.architectureGrid}`}>
            <div className={styles.architectureCopy}>
              <p className={styles.kicker}>Composable by boundary</p>
              <h2>Use Kubernetes as the contract.</h2>
              <p>
                Application owners declare serving intent. Cluster administrators publish
                reusable runtime profiles. Noctaya translates both into the workloads and
                lifecycle resources your cluster already understands.
              </p>
              <div className={styles.boundaryList}>
                {boundaries.map(([title, description]) => (
                  <div key={title}>
                    <strong>{title}</strong>
                    <span>{description}</span>
                  </div>
                ))}
              </div>
              <Link className={styles.textLink} to="/docs/architecture">
                Read the architecture guide <ArrowIcon />
              </Link>
            </div>
            <div className={styles.flowCard}>
              <div className={styles.flowTop}>
                <span>LLMService</span>
                <b>+</b>
                <span>InferenceRuntime</span>
              </div>
              <div className={styles.flowConnector}><i /></div>
              <div className={styles.flowCore}>
                <div><strong>Noctaya</strong><span>reconcile · activate · drain</span></div>
              </div>
              <div className={styles.flowConnector}><i /></div>
              <div className={styles.flowOutput}>
                <span>Gateway</span>
                <span>Backend 0..N</span>
                <span>Cache</span>
                <span>KEDA</span>
              </div>
            </div>
          </div>
        </section>

        <section className={styles.kthenaSection} id="noctaya-and-kthena">
          <div className={styles.sectionShell}>
            <div className={styles.policyHeader}>
              <p className={styles.kicker}>One cluster, two serving policies</p>
              <h2>Keep the hot path hot. Let the long tail sleep.</h2>
            </div>
            <div className={styles.policyGrid}>
              <article>
                <span className={styles.policyTag}>High traffic</span>
                <h3>Kthena</h3>
                <p>
                  Fleet routing, cache-aware scheduling, disaggregation, and continuously ready
                  models.
                </p>
                <div className={styles.hotLine}><i /><span>ready</span></div>
              </article>
              <div className={styles.policyPlus}>+</div>
              <article>
                <span className={styles.policyTag}>Long tail</span>
                <h3>Noctaya</h3>
                <p>
                  A small declarative control plane for occasional models that should release
                  their accelerators.
                </p>
                <div className={styles.coldLine}><i /><span>0 → 1 → 0</span></div>
              </article>
            </div>
            <Link className={styles.textLink} to="/docs/demo">
              See the operational walkthrough <ArrowIcon />
            </Link>
          </div>
        </section>

        <section className={styles.ctaSection}>
          <div className={styles.ctaCard}>
            <div>
              <p className={styles.kicker}>Try Noctaya</p>
              <h2>Start with one model and one runtime.</h2>
              <p>Install the chart, select a hardware profile, and watch the backend wake from zero.</p>
            </div>
            <div className={styles.ctaActions}>
              <Link className={styles.primaryAction} to="/docs/started">
                Open the Quick Start <ArrowIcon />
              </Link>
              <Link
                className={styles.secondaryAction}
                to="/CONTRIBUTING"
              >
                Contribute
              </Link>
            </div>
          </div>
        </section>
      </main>
    </Layout>
  );
}

export default Home;
