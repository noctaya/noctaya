import React from 'react';
import Head from '@docusaurus/Head';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';
import styles from './index.module.css';

const coldPath = [
  {
    number: '01',
    title: 'Schedule',
    description: 'Place the backend on a node with the requested accelerator.',
  },
  {
    number: '02',
    title: 'Pull',
    description: 'Fetch runtime layers when the selected node has no warm image.',
  },
  {
    number: '03',
    title: 'Load',
    description: 'Mount the model cache, load weights, and initialize the runtime.',
  },
  {
    number: '04',
    title: 'Ready',
    description: 'Release admitted traffic only after model-aware probes succeed.',
  },
];

const protections = [
  {
    title: 'Preserved activation',
    description:
      'An activation lease keeps demand visible while the backend starts, even if the first client disconnects.',
  },
  {
    title: 'Bounded admission',
    description:
      'The gateway returns a clear 429 before queued connections can become the next failure.',
  },
  {
    title: 'Streaming-safe waits',
    description:
      'SSE heartbeats protect long cold starts, while reject mode gives clients an explicit retry path.',
  },
  {
    title: 'Graceful return to zero',
    description:
      'Readiness gates new traffic and drain hooks protect in-flight streams during scale-down.',
  },
];

const ownership = [
  {
    label: 'Application',
    title: 'Serving intent',
    description: 'Model, runtime selector, resources, cache, scaling, and endpoint behavior.',
  },
  {
    label: 'Noctaya',
    title: 'Model lifecycle',
    description: 'Reconciliation, cold activation, admission, readiness, and graceful drain.',
  },
  {
    label: 'Cluster',
    title: 'Infrastructure',
    description: 'Inference engines, device plugins, schedulers, KEDA, and monitoring.',
  },
];

const validationTargets = [
  'NVIDIA A10',
  'Atlas 300I Duo',
  'Atlas 300I Pro',
  'Ascend 910B3',
];

function ArrowIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true">
      <path
        d="M4 10h11M11 5l5 5-5 5"
        fill="none"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.8"
      />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true">
      <path
        d="m4 10 4 4 8-9"
        fill="none"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="2"
      />
    </svg>
  );
}

function LifecyclePanel() {
  return (
    <div className={styles.lifecyclePanel} aria-label="Noctaya scale-to-zero lifecycle">
      <div className={styles.panelHeader}>
        <div className={styles.windowDots} aria-hidden="true">
          <i />
          <i />
          <i />
        </div>
        <span>llmservice / qwen-longtail</span>
        <span className={styles.endpointBadge}>
          <i />
          endpoint online
        </span>
      </div>

      <div className={styles.panelBody}>
        <div className={styles.resourceSummary}>
          <span>
            <small>Phase</small>
            <strong>ScaledToZero</strong>
          </span>
          <span>
            <small>Gateway</small>
            <strong>1 / 1 ready</strong>
          </span>
          <span>
            <small>Backend</small>
            <strong>0 / 2 active</strong>
          </span>
        </div>

        <div className={styles.requestLine}>
          <span>next request</span>
          <i />
          <strong>activation lease</strong>
        </div>

        <div className={styles.scaleJourney}>
          <div className={styles.scaleState}>
            <span>idle</span>
            <strong>0</strong>
            <small>accelerators used</small>
          </div>
          <div className={styles.journeyArrow}>
            <span>demand</span>
            <i />
          </div>
          <div className={`${styles.scaleState} ${styles.scaleStateActive}`}>
            <span>serve</span>
            <strong>1..N</strong>
            <small>ready backends</small>
          </div>
          <div className={styles.journeyArrow}>
            <span>quiet</span>
            <i />
          </div>
          <div className={styles.scaleState}>
            <span>release</span>
            <strong>0</strong>
            <small>accelerators used</small>
          </div>
        </div>

        <div className={styles.signalPath}>
          <span>
            <i className={styles.gatewaySignal} />
            Gateway
            <small>admit + hold</small>
          </span>
          <b aria-hidden="true" />
          <span>
            <i className={styles.kedaSignal} />
            KEDA
            <small>scale backend</small>
          </span>
          <b aria-hidden="true" />
          <span>
            <i className={styles.readySignal} />
            Model
            <small>forward when ready</small>
          </span>
        </div>
      </div>

      <div className={styles.panelCommand}>
        <span>$</span>
        <code>kubectl get llmservice qwen-longtail -w</code>
        <strong>0 → 1..N → 0</strong>
      </div>
    </div>
  );
}

function ArchitectureMap() {
  return (
    <div className={styles.architectureMap} aria-label="Noctaya architecture">
      <div className={styles.intentRow}>
        <span>
          <small>Namespaced intent</small>
          LLMService
        </span>
        <b>+</b>
        <span>
          <small>Reusable profile</small>
          InferenceRuntime
        </span>
      </div>

      <div className={styles.verticalRoute}>
        <i />
        <small>reconcile</small>
      </div>

      <div className={styles.operatorNode}>
        <span className={styles.operatorPulse} />
        <div>
          <strong>Noctaya operator</strong>
          <small>Kubernetes lifecycle translation</small>
        </div>
      </div>

      <div className={styles.resourceRoutes} aria-hidden="true">
        <i />
      </div>

      <div className={styles.resourceGrid}>
        <span>
          <small>Always on</small>
          Gateway
        </span>
        <span>
          <small>Scale 0..N</small>
          Model backend
        </span>
        <span>
          <small>Optional</small>
          Cache + prewarm
        </span>
        <span>
          <small>Rendered API</small>
          ScaledObject
        </span>
      </div>

      <div className={styles.kedaPeer}>
        <span>KEDA</span>
        <small>required · installed independently</small>
      </div>
    </div>
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
        <meta name="theme-color" content="#100a1b" />
      </Head>

      <main className={styles.homepage}>
        <section className={styles.hero}>
          <div className={styles.heroBackdrop} aria-hidden="true">
            <i />
            <i />
            <i />
          </div>
          <div className={`${styles.shell} ${styles.heroGrid}`}>
            <div className={styles.heroCopy}>
              <Link className={styles.releasePill} to="/ROADMAP">
                <span />
                v0.4.0-alpha.1
                <i />
                production hardening
                <ArrowIcon />
              </Link>
              <h1>
                Scale idle LLMs to zero.
                <span>Wake them on demand.</span>
              </h1>
              <p className={styles.heroLead}>
                Noctaya is a minimal Kubernetes control plane for bursty and long-tail models. It
                keeps one stable OpenAI-compatible endpoint while releasing accelerators between
                requests.
              </p>
              <div className={styles.heroActions}>
                <Link className={styles.primaryAction} to="/docs/getting-started">
                  Get started
                  <ArrowIcon />
                </Link>
                <Link className={styles.secondaryAction} to="/docs/architecture">
                  See how it works
                </Link>
              </div>
              <div className={styles.heroProof}>
                <span>
                  <CheckIcon /> Kubernetes-native API
                </span>
                <span>
                  <CheckIcon /> NVIDIA and Ascend
                </span>
                <span>
                  <CheckIcon /> Apache-2.0
                </span>
              </div>
            </div>

            <LifecyclePanel />
          </div>
        </section>

        <section className={styles.validationStrip} aria-label="Physical validation targets">
          <div className={styles.shell}>
            <span className={styles.stripLabel}>Physical validation recorded for</span>
            <div className={styles.validationTargets}>
              {validationTargets.map((target) => (
                <span key={target}>
                  <i />
                  {target}
                </span>
              ))}
            </div>
            <Link to="/docs/validation/requirements">
              Evidence <ArrowIcon />
            </Link>
          </div>
        </section>

        <section className={styles.coldPathSection}>
          <div className={styles.shell}>
            <div className={styles.sectionHeading}>
              <div>
                <p className={styles.kicker}>The cold path is the product</p>
                <h2>Zero is one state. Recovery is a pipeline.</h2>
              </div>
              <p>
                Scheduling, image pull, weight load, and readiness each have a different tail.
                Noctaya keeps that entire path bounded, observable, and safe for the gateway.
              </p>
            </div>

            <div className={styles.coldPathRail}>
              {coldPath.map((stage) => (
                <article key={stage.number}>
                  <div>
                    <span>{stage.number}</span>
                    <i />
                  </div>
                  <h3>{stage.title}</h3>
                  <p>{stage.description}</p>
                </article>
              ))}
            </div>

            <div className={styles.protectionGrid}>
              {protections.map((protection) => (
                <article key={protection.title}>
                  <span>
                    <CheckIcon />
                  </span>
                  <div>
                    <h3>{protection.title}</h3>
                    <p>{protection.description}</p>
                  </div>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className={styles.architectureSection}>
          <div className={`${styles.shell} ${styles.architectureGrid}`}>
            <div className={styles.architectureCopy}>
              <p className={styles.kicker}>Small by design</p>
              <h2>Use Kubernetes as the contract.</h2>
              <p>
                Noctaya translates portable serving intent into the workloads and lifecycle
                resources your cluster already understands. It does not replace your inference
                engine, device plugin, scheduler, or monitoring stack.
              </p>

              <div className={styles.ownershipList}>
                {ownership.map((item) => (
                  <article key={item.label}>
                    <span>{item.label}</span>
                    <div>
                      <strong>{item.title}</strong>
                      <small>{item.description}</small>
                    </div>
                  </article>
                ))}
              </div>

              <Link className={styles.textLink} to="/docs/architecture">
                Read the architecture guide <ArrowIcon />
              </Link>
            </div>

            <ArchitectureMap />
          </div>
        </section>

        <section className={styles.compositionSection} id="noctaya-and-kthena">
          <div className={`${styles.shell} ${styles.compositionGrid}`}>
            <div>
              <p className={styles.kicker}>Composable, not all-encompassing</p>
              <h2>Fit Noctaya beside the stack you already operate.</h2>
              <p className={styles.compositionLead}>
                Components keep clear ownership. KEDA scales. Runtime images infer. Device plugins
                expose accelerators. Schedulers place workloads. Noctaya coordinates the per-model
                lifecycle.
              </p>
              <div className={styles.ecosystemGrid}>
                <article>
                  <span>Required peer</span>
                  <strong>KEDA</strong>
                  <small>Installed and managed independently</small>
                </article>
                <article>
                  <span>Runtime</span>
                  <strong>vLLM + vendor plugins</strong>
                  <small>Existing inference images stay in place</small>
                </article>
                <article>
                  <span>Cluster-owned</span>
                  <strong>Device plugins + schedulers</strong>
                  <small>Including optional Volcano placement</small>
                </article>
                <article>
                  <span>On demand</span>
                  <strong>Prometheus + Grafana</strong>
                  <small>Observability remains outside reconciliation</small>
                </article>
              </div>
            </div>

            <aside className={styles.coexistCard}>
              <div className={styles.coexistHeader}>
                <span>One cluster</span>
                <strong>Different serving policies</strong>
              </div>
              <div className={styles.policyCard}>
                <div>
                  <span className={styles.hotDot} />
                  <small>latency-sensitive models</small>
                </div>
                <strong>Keep continuously ready</strong>
                <p>Let a fleet-serving platform own the hot path.</p>
                <i className={styles.hotTrack} />
              </div>
              <div className={styles.policyDivider}>
                <span>separate workloads · shared cluster</span>
              </div>
              <div className={styles.policyCard}>
                <div>
                  <span className={styles.coldDot} />
                  <small>bursty or long-tail models</small>
                </div>
                <strong>Release between requests</strong>
                <p>Let Noctaya own a distinct endpoint and backend.</p>
                <i className={styles.coldTrack} />
              </div>
              <p className={styles.coexistNote}>
                Kthena is one validated coexistence example; the boundary is platform-neutral.
              </p>
            </aside>
          </div>
        </section>

        <section className={styles.evidenceSection}>
          <div className={`${styles.shell} ${styles.evidenceCard}`}>
            <div>
              <p className={styles.kicker}>Evidence before claims</p>
              <h2>Validation tied to recorded stacks.</h2>
              <p>
                Hardware reports name the device, topology, software versions, commands, and
                observed lifecycle. Rendering tests never stand in for a physical result.
              </p>
            </div>
            <div className={styles.evidenceLinks}>
              <Link to="/docs/validation/nvidia/a10">
                NVIDIA A10 report <ArrowIcon />
              </Link>
              <Link to="/docs/validation/ascend/310p">
                Ascend 310P report <ArrowIcon />
              </Link>
              <Link to="/docs/validation/ascend/910b3">
                Ascend 910B3 report <ArrowIcon />
              </Link>
            </div>
          </div>
        </section>

        <section className={styles.ctaSection}>
          <div className={`${styles.shell} ${styles.ctaCard}`}>
            <div>
              <p className={styles.kicker}>Start with one model</p>
              <h2>Build the complete 0 → 1 → N → 0 lifecycle.</h2>
              <p>
                Install Noctaya and KEDA independently, select a hardware profile, and follow the
                backend from idle through inference and back to zero.
              </p>
            </div>
            <div className={styles.ctaActions}>
              <Link className={styles.primaryAction} to="/docs/getting-started">
                Open Getting Started <ArrowIcon />
              </Link>
              <Link className={styles.secondaryAction} to="/CONTRIBUTING">
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
