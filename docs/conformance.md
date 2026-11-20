# Scheduler conformance

The conformance tests show which kinds of workload a scheduler handles, such as gang scheduled jobs. Each test runs a short workflow on virtual nodes and either passes or fails. A pass means the scheduler supports that kind of workload. A failure means it does not, or that the test could not run, and the runner prints the error.

| Test | Pass when |
|------|-----------|
| [job-gang](../resources/conformance/job-gang) | a 3-pod gang that fits on only 2 nodes places no pod, and all 3 pods run once a third node opens |

## Running the tests

The runner uses the current cluster. Install KWOK and the scheduler first, then pass the scheduler's name:

```bash
source scripts/env.sh && deploy_kwok
./scripts/schedulers/volcano.sh
./scripts/conformance.sh volcano
```

`k8s` tests the default scheduler, which needs no install. `tests/ci/test_conformance.sh <scheduler>` runs both steps, as the nightly job does for each scheduler.

The runner runs every test that has a workflow for the scheduler and prints `supports` or `does not support` for each. Each test runs in the `conformance` namespace, which the runner deletes afterward. Use a cluster you can spare, since tests cordon and add nodes.

## Grafana

With `CONFORMANCE_PUSHGATEWAY` set, the runner pushes its results to that Pushgateway (see [Prometheus and Grafana](metrics.md#prometheus-and-grafana)):

```bash
kubectl -n monitoring port-forward svc/pushgateway-prometheus-pushgateway 9091 &
CONFORMANCE_PUSHGATEWAY=http://localhost:9091 ./scripts/conformance.sh volcano
```

Import [`resources/dashboards/conformance.json`](../resources/dashboards/conformance.json). It shows one row per scheduler and one box per test: green for supports, red for does not support, and blank for not tested. Click a box to open the test. A second table lists what each test needs to pass. Each run replaces that scheduler's previous results.

The runner pushes two series per test, with a `scheduler` label from the push:

- `knavigator_conformance{test}`: 1 when the test passed and 0 when it failed.
- `knavigator_conformance_criteria{test, pass}`: always 1. `pass` holds the test's pass condition.

## Adding a test

A test is a directory under `resources/conformance`:

```
resources/conformance/<test>/
  README.md          a line starting "Pass: " that says what passing means, then how the test works
  <scheduler>.yaml   the workflow for one scheduler, one file per scheduler
  setup.sh           optional; installs what the test needs, such as an operator
```

Other files, such as templates, can sit next to them. Workflows name templates relative to the test directory. The runner shows the `Pass: ` line in Grafana, so keep it to one plain sentence.

A workflow fails the test when any task fails. To show that a scheduler holds pods back, check that none of them run. `CheckPod` with `count` expects exactly that many pods in `status` and ignores the rest:

```yaml
- id: none-running
  type: CheckPod
  params:
    refTaskId: job
    status: Running
    count: 0
```

`count` checks once, so wait with a `Sleep` task before it.

## Adding a scheduler

Add an install script at `scripts/schedulers/<scheduler>.sh`, then a `<scheduler>.yaml` workflow to each test the scheduler should run. Tests without one show as not tested. Add the scheduler to the `conformance` matrix in `.github/workflows/nightly.yml`.

## Tests outside this repository

The runner takes a tests directory as its second argument, laid out as above, so tests can live elsewhere:

```bash
./scripts/conformance.sh my-scheduler ~/my-conformance-tests
```
