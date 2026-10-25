## Example of running `slurm-bridge` with `knavigator`

[slurm-bridge](https://github.com/SlinkyProject/slurm-bridge) lets Slurm place Kubernetes pods:
each pod, or each PodGroup, becomes a Slurm job, and slurm-bridge's scheduler binds the pod to the
node Slurm picked. The install runs slurmctld and slurmrestd in the cluster through
[slurm-operator](https://github.com/SlinkyProject/slurm-operator); the nodes are KWOK nodes that
slurm-bridge registers as Slurm external nodes.

### Install

`scripts/schedulers/slurm-bridge.sh` installs slurm-bridge v1.2.3 and Slurm 26.05.4 from the
charts in `oci://ghcr.io/slinkyproject/charts`, pinned by digest, with the values in
`charts/overrides/slurm-bridge/`. Set `SLURM_BRIDGE_VERSION` to install another release by tag.
The digest-pinned chart references need helm v3.18.0 or later: helm v3.16 rejects them, and helm
v3.17 accepts them but ignores the digest.
The script generates the Slurm auth key and the JWT key into the `slurm-auth-slurm` and
`slurm-auth-jwt` Secrets on first install, and applies the KWOK stage `pod-fail-on-delete`, which
fails a deleted pod that still carries slurm-bridge's finalizer, as a kubelet would.

On a cluster with KWOK and its pod-complete stage, as `scripts/create-test-cluster.sh` sets up,
run it on its own or pick it from the menu:
```shell
./scripts/schedulers/slurm-bridge.sh
```

### Running a workflow with `Job`

A node joins Slurm when it carries the label `scheduler.slinky.slurm.net/external-node: "true"`,
with its partitions in the annotation `scheduler.slinky.slurm.net/external-node-partitions` and its
leaf switch in `topology.slinky.slurm.net/spec`. The `Configure` task of the example sets all
three, then the workflow submits a 3-pod Job and checks that the pods run:
```shell
./bin/knavigator -workflow resources/workflows/slurm-bridge/test-job.yaml -cleanup
```

The job template takes `timeLimit`, the Slurm time limit in minutes, and optional `partition` and
`priority`. `timeLimit` must outlast `ttl`: when Slurm ends the job at its time limit, slurm-bridge
deletes the pod, which ends Failed instead of completing. Partition `slurm-bridge-preempt` may
preempt jobs of the default partition `slurm-bridge`.

### Limits

- Slurm sees whole nodes, not GPUs. The template sets an empty `slurmjob.slinky.slurm.net/gres`,
  so the Slurm job drops the pod's GPU request, and each pod should ask for a whole node's GPUs.
- `-results` records only pods that request `nvidia.com/gpu`. The pods of this template keep their
  GPU request in Kubernetes, so `-results` sees them; a slurm-bridge template that drops the
  request from the pod as well would make its pods invisible to `-results`.
