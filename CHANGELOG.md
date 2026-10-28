# Changelog

Notable changes to Knavigator, organized by release and linked to the merged pull requests that introduced them. Related PRs are grouped into a single entry.

Release membership follows Git tag ancestry. Release dates are GitHub publication dates; Helm chart versions are tracked independently from application releases.

## [Unreleased](https://github.com/NVIDIA/knavigator/compare/v0.1.0...main)

### Added

- KAI Scheduler support, plus RayJob and MPIJob workflows for KAI and Kueue. ([#120](https://github.com/NVIDIA/knavigator/pull/120), [#122](https://github.com/NVIDIA/knavigator/pull/122))
- Scripts for running scaling benchmarks. ([#123](https://github.com/NVIDIA/knavigator/pull/123))
- `-results <dir>` to record pod timelines in `timeline.csv` and scheduling metrics in `results.json`, including makespan, GPU-hours, utilization, and pod/job wait times. Job wait includes time queued before pods are created. ([#133](https://github.com/NVIDIA/knavigator/pull/133))
- `-speedup` to compress workflow sleeps and submitted job runtimes, with result durations reported in simulated time. Scheduler processing still runs in real time, so compressed runs have latency-measurement limits. ([#134](https://github.com/NVIDIA/knavigator/pull/134))
- `SubmitTrace` for reproducible workloads generated from seeded job classes, with mixed GPU requirements, gang sizes, priorities, accounts, and runtimes. Classes also support delayed starts, regular arrivals, and simultaneous bursts. ([#138](https://github.com/NVIDIA/knavigator/pull/138), [#171](https://github.com/NVIDIA/knavigator/pull/171))
- Virtual cluster configuration for racks, GPU counts and products, taints, resource overrides, and DRA ResourceSlices. Helm now honors `-kubectx` and `-kubeconfig`; `KNAVIGATOR_VIRTUAL_NODES_CHART` selects a local chart. The virtual-nodes chart version is now `0.3.0`. ([#137](https://github.com/NVIDIA/knavigator/pull/137))
- Live run metrics through `-results.pushgateway`, a run-results Grafana dashboard, job counts and waits by gang size, and GPU occupancy dashboard totals for idle and wasted GPU-hours. ([#149](https://github.com/NVIDIA/knavigator/pull/149))
- Eviction and preemption reporting, GPU time lost to evictions and gang binding, and an `events.csv` file of warning, eviction, and preemption events. `-results.stall` can stop a run when placement and completion make no progress. Evicted pods no longer count as completed work. ([#148](https://github.com/NVIDIA/knavigator/pull/148))
- Job-size fairness metrics: wait and bounded slowdown by GPU footprint, size/wait correlation, large-job starvation ratio, and unadmitted or abandoned jobs. `-results.bsld-floor` configures the slowdown runtime floor. ([#140](https://github.com/NVIDIA/knavigator/pull/140))
- Wait statistics and SLO attainment by job class, with class labels in Pushgateway metrics. Jobs that never start count as SLO misses. ([#169](https://github.com/NVIDIA/knavigator/pull/169))
- `-aggregate '<glob>'` to summarize repeated runs with mean, sample standard deviation, range, coefficient of variation, bootstrap confidence intervals, and a stability indicator. ([#170](https://github.com/NVIDIA/knavigator/pull/170))
- `pkg/eventlog` with a validated JSON Lines event format, schema version 1, and strict reading of sequences, timestamps, fields, and transitions. This adds the package; run-time `events.jsonl` output is not yet integrated. ([#157](https://github.com/NVIDIA/knavigator/pull/157))

### Changed

- Runs using `-results` now exit with an error after writing results if work does not drain, a pod fails, or a job never starts. Results include `pods_failed`. ([#139](https://github.com/NVIDIA/knavigator/pull/139))
- Each workload manager has a standalone installer under `scripts/schedulers/`. Cluster creation discovers these scripts for its selection menu, and nightly tests invoke them directly. ([#154](https://github.com/NVIDIA/knavigator/pull/154))
- Network topology benchmark labels now use the `network.topology.nvidia.com` prefix. ([#119](https://github.com/NVIDIA/knavigator/pull/119))

### Fixed

- With `-results -cleanup`, objects remain until all workflows finish and results are written, then cleanup runs once. This prevents deleted pods from producing incorrect completion times and zero GPU-hours. ([#135](https://github.com/NVIDIA/knavigator/pull/135))
- Negative job wait times caused by timestamps with second-level precision. ([#136](https://github.com/NVIDIA/knavigator/pull/136))
- `RegisterObj` resource discovery for built-in and custom kinds at the template's exact API group and version, including scheduling Workload and PodGroup resources. ([#156](https://github.com/NVIDIA/knavigator/pull/156))
- Workload-manager menu validation rejects invalid numbers and nonnumeric input before indexing the installer list. ([#165](https://github.com/NVIDIA/knavigator/pull/165))
- Repeated MPI operator installation succeeds when aggregated ClusterRole rules are already managed by the cluster role aggregation controller. ([#167](https://github.com/NVIDIA/knavigator/pull/167))
- KWOK installation keeps kind's `kindnet` and `kube-proxy` DaemonSets off virtual nodes, preventing repeated pod creation and deletion from distorting benchmark results. ([#175](https://github.com/NVIDIA/knavigator/pull/175))

### Maintenance and dependencies

- Upgrade Go through `1.25.11`, `1.26.6`, and `1.27.2` to `1.27.3`. ([#129](https://github.com/NVIDIA/knavigator/pull/129), [#131](https://github.com/NVIDIA/knavigator/pull/131), [#141](https://github.com/NVIDIA/knavigator/pull/141), [#153](https://github.com/NVIDIA/knavigator/pull/153))
- Update `golang.org/x/net` through `0.36.0`, `0.38.0`, and `0.55.0`; the later Go upgrade above also updates it to `0.60.0`. ([#118](https://github.com/NVIDIA/knavigator/pull/118), [#121](https://github.com/NVIDIA/knavigator/pull/121), [#128](https://github.com/NVIDIA/knavigator/pull/128))
- Refresh third-party components and Go dependencies, including Prometheus common `0.72.1`, and add issue templates. ([#117](https://github.com/NVIDIA/knavigator/pull/117), [#127](https://github.com/NVIDIA/knavigator/pull/127), [#142](https://github.com/NVIDIA/knavigator/pull/142), [#160](https://github.com/NVIDIA/knavigator/pull/160))
- Update chart-releaser to `1.7.0`, Docker build-push to `7.4.0`, Docker metadata to `6.2.0`, Codecov to `v7`, and Docker login through `4.6.0` to `4.7.0`. ([#143](https://github.com/NVIDIA/knavigator/pull/143), [#144](https://github.com/NVIDIA/knavigator/pull/144), [#145](https://github.com/NVIDIA/knavigator/pull/145), [#146](https://github.com/NVIDIA/knavigator/pull/146), [#147](https://github.com/NVIDIA/knavigator/pull/147), [#163](https://github.com/NVIDIA/knavigator/pull/163))
- Update setup-helm to `v5`, setup-go to `v7`, and checkout to `v7`. ([#161](https://github.com/NVIDIA/knavigator/pull/161), [#162](https://github.com/NVIDIA/knavigator/pull/162), [#164](https://github.com/NVIDIA/knavigator/pull/164))
- Configure GitHub runners and add a security reporting policy. ([#132](https://github.com/NVIDIA/knavigator/pull/132), [#130](https://github.com/NVIDIA/knavigator/pull/130))

## [v0.1.0](https://github.com/NVIDIA/knavigator/releases/tag/v0.1.0) — 2025-01-02

Initial published release, containing changes through December 30, 2024 (tagged commit `3d1e1cc`).

### Added

- Custom-resource deletion and reusable object registration with `RegisterObj`. Pod counts support arithmetic expressions, and template files can contain multiple objects. ([#4](https://github.com/NVIDIA/knavigator/pull/4), [#33](https://github.com/NVIDIA/knavigator/pull/33), [#53](https://github.com/NVIDIA/knavigator/pull/53), [#99](https://github.com/NVIDIA/knavigator/pull/99))
- Virtual-node Helm chart and task-based node configuration, with extensible node types, multiple entries of the same type, and node selectors for `UpdateNodes`. ([#6](https://github.com/NVIDIA/knavigator/pull/6), [#46](https://github.com/NVIDIA/knavigator/pull/46), [#47](https://github.com/NVIDIA/knavigator/pull/47), [#97](https://github.com/NVIDIA/knavigator/pull/97), [#103](https://github.com/NVIDIA/knavigator/pull/103))
- `Configure` workflows for creating and deleting namespaces, managing ConfigMaps and PriorityClasses, and restarting deployments. ([#50](https://github.com/NVIDIA/knavigator/pull/50), [#56](https://github.com/NVIDIA/knavigator/pull/56), [#64](https://github.com/NVIDIA/knavigator/pull/64), [#83](https://github.com/NVIDIA/knavigator/pull/83), [#93](https://github.com/NVIDIA/knavigator/pull/93))
- `-cleanup` and cleanup timeout options to remove submitted resources after a test. ([#37](https://github.com/NVIDIA/knavigator/pull/37))
- Kubernetes client QPS and burst configuration flags, plus usage output when no arguments are supplied. ([#32](https://github.com/NVIDIA/knavigator/pull/32), [#14](https://github.com/NVIDIA/knavigator/pull/14))
- HTTP server mode for workflow execution and the `klient` command-line client. ([#58](https://github.com/NVIDIA/knavigator/pull/58))
- Job time-to-live support, a `CheckConfigmap` task, and an option to skip submission of existing objects. ([#73](https://github.com/NVIDIA/knavigator/pull/73), [#76](https://github.com/NVIDIA/knavigator/pull/76), [#84](https://github.com/NVIDIA/knavigator/pull/84))
- Kueue examples and preemption tests, YuniKorn templates and workflows, and Run:ai templates and workflows. ([#25](https://github.com/NVIDIA/knavigator/pull/25), [#54](https://github.com/NVIDIA/knavigator/pull/54), [#65](https://github.com/NVIDIA/knavigator/pull/65), [#87](https://github.com/NVIDIA/knavigator/pull/87))
- Gang-scheduling benchmark configurations, including the coscheduling plugin with JobSet and Kueue. ([#74](https://github.com/NVIDIA/knavigator/pull/74), [#98](https://github.com/NVIDIA/knavigator/pull/98))
- Scaling benchmarks and benchmark runner scripts. ([#95](https://github.com/NVIDIA/knavigator/pull/95), [#106](https://github.com/NVIDIA/knavigator/pull/106))
- Network topology tests for Run:ai and JobSet, including required pod affinity with `requiredDuringSchedulingIgnoredDuringExecution`. ([#104](https://github.com/NVIDIA/knavigator/pull/104), [#110](https://github.com/NVIDIA/knavigator/pull/110), [#112](https://github.com/NVIDIA/knavigator/pull/112))
- Node Resource Exporter with a Helm chart, and a Grafana dashboard for GPU occupancy. ([#114](https://github.com/NVIDIA/knavigator/pull/114), [#116](https://github.com/NVIDIA/knavigator/pull/116))
- Kubernetes job failure-injection example. ([#9](https://github.com/NVIDIA/knavigator/pull/9))

### Changed

- Rename task configuration to workflow terminology, including the command-line interface and resource directory. ([#59](https://github.com/NVIDIA/knavigator/pull/59))
- Support brace-expanded file paths. ([#94](https://github.com/NVIDIA/knavigator/pull/94))
- Simplify logging and add diagnostic logging to `CheckConfigmap`. ([#75](https://github.com/NVIDIA/knavigator/pull/75), [#79](https://github.com/NVIDIA/knavigator/pull/79))
- Keep installed controller deployments off KWOK virtual nodes using anti-affinity. ([#96](https://github.com/NVIDIA/knavigator/pull/96))
- Refresh gang-scheduling templates, benchmark configurations, JobSet configurations, and benchmark tests. ([#89](https://github.com/NVIDIA/knavigator/pull/89), [#91](https://github.com/NVIDIA/knavigator/pull/91), [#92](https://github.com/NVIDIA/knavigator/pull/92), [#100](https://github.com/NVIDIA/knavigator/pull/100), [#101](https://github.com/NVIDIA/knavigator/pull/101), [#105](https://github.com/NVIDIA/knavigator/pull/105), [#111](https://github.com/NVIDIA/knavigator/pull/111), [#113](https://github.com/NVIDIA/knavigator/pull/113))
- Refresh installation scripts. ([#85](https://github.com/NVIDIA/knavigator/pull/85), [#86](https://github.com/NVIDIA/knavigator/pull/86))

### Fixed

- Object deletion errors, cleanup of already-deleted objects, lookup of missing objects, and deletion of missing ConfigMaps. ([#41](https://github.com/NVIDIA/knavigator/pull/41), [#45](https://github.com/NVIDIA/knavigator/pull/45), [#66](https://github.com/NVIDIA/knavigator/pull/66), [#80](https://github.com/NVIDIA/knavigator/pull/80))
- Allow conditional object templates and omit `nameFormat` for single-instance objects. ([#55](https://github.com/NVIDIA/knavigator/pull/55))
- Pod completion timing honors job TTL through the KWOK jitter-delay annotation. ([#78](https://github.com/NVIDIA/knavigator/pull/78))
- Failure-injection job pod naming matches `CheckPod` expectations, and Kueue preemption tests include pod checks. ([#88](https://github.com/NVIDIA/knavigator/pull/88), [#57](https://github.com/NVIDIA/knavigator/pull/57))
- Default virtual-node installation values and deployment instructions. ([#11](https://github.com/NVIDIA/knavigator/pull/11))
- Unit tests, lint findings, YAML formatting, spelling, and a YuniKorn preemption workflow typo. ([#7](https://github.com/NVIDIA/knavigator/pull/7), [#22](https://github.com/NVIDIA/knavigator/pull/22), [#23](https://github.com/NVIDIA/knavigator/pull/23), [#27](https://github.com/NVIDIA/knavigator/pull/27), [#28](https://github.com/NVIDIA/knavigator/pull/28), [#29](https://github.com/NVIDIA/knavigator/pull/29), [#70](https://github.com/NVIDIA/knavigator/pull/70))

### Documentation and examples

- Initial public README and documentation, project logo and badges, in-cluster usage instructions, and copyable getting-started commands. ([#1](https://github.com/NVIDIA/knavigator/pull/1), [#5](https://github.com/NVIDIA/knavigator/pull/5), [#16](https://github.com/NVIDIA/knavigator/pull/16), [#17](https://github.com/NVIDIA/knavigator/pull/17), [#18](https://github.com/NVIDIA/knavigator/pull/18), [#51](https://github.com/NVIDIA/knavigator/pull/51), [#67](https://github.com/NVIDIA/knavigator/pull/67))
- Update examples for `RegisterObj`, simplify job examples, and refresh Kueue instructions and workflows. ([#38](https://github.com/NVIDIA/knavigator/pull/38), [#42](https://github.com/NVIDIA/knavigator/pull/42), [#62](https://github.com/NVIDIA/knavigator/pull/62))
- Use Helm template comments and document the default Prometheus namespace. ([#8](https://github.com/NVIDIA/knavigator/pull/8), [#10](https://github.com/NVIDIA/knavigator/pull/10))
- Add and update demo tooling and recordings; remove the obsolete demo before the release. ([#13](https://github.com/NVIDIA/knavigator/pull/13), [#44](https://github.com/NVIDIA/knavigator/pull/44), [#115](https://github.com/NVIDIA/knavigator/pull/115))
- Remove NVIDIA-internal NGC templates and tests. ([#2](https://github.com/NVIDIA/knavigator/pull/2))

### Maintenance and dependencies

- Add Go lint and unit-test CI, coverage reporting, Kubernetes job integration tests, nightly scheduler tests, and JobSet/YuniKorn CI coverage. ([#15](https://github.com/NVIDIA/knavigator/pull/15), [#35](https://github.com/NVIDIA/knavigator/pull/35), [#39](https://github.com/NVIDIA/knavigator/pull/39), [#52](https://github.com/NVIDIA/knavigator/pull/52), [#68](https://github.com/NVIDIA/knavigator/pull/68), [#69](https://github.com/NVIDIA/knavigator/pull/69))
- Add manual Docker image publishing and a Helm chart release workflow. ([#20](https://github.com/NVIDIA/knavigator/pull/20), [#102](https://github.com/NVIDIA/knavigator/pull/102))
- Configure golangci-lint and replace its deprecated `megacheck` setting. ([#24](https://github.com/NVIDIA/knavigator/pull/24), [#43](https://github.com/NVIDIA/knavigator/pull/43))
- Add verification scripts, then revert their addition before the release. ([#21](https://github.com/NVIDIA/knavigator/pull/21), [#30](https://github.com/NVIDIA/knavigator/pull/30))
- Add resource license headers and CODEOWNERS. ([#63](https://github.com/NVIDIA/knavigator/pull/63), [#108](https://github.com/NVIDIA/knavigator/pull/108))
- Update `golang.org/x/net` to `0.23.0`, JobSet documentation to `0.5.2`, KWOK, Kueue to `0.8.0`, YuniKorn to `1.5.2`, and other workload-manager and third-party dependencies. ([#3](https://github.com/NVIDIA/knavigator/pull/3), [#61](https://github.com/NVIDIA/knavigator/pull/61), [#77](https://github.com/NVIDIA/knavigator/pull/77), [#81](https://github.com/NVIDIA/knavigator/pull/81), [#90](https://github.com/NVIDIA/knavigator/pull/90), [#107](https://github.com/NVIDIA/knavigator/pull/107), [#109](https://github.com/NVIDIA/knavigator/pull/109))
