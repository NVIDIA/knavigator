# job-gang

Pass: a 3-pod gang that fits on only 2 nodes places no pod, and all 3 pods run once a third node opens.

The workflow configures 3 nodes with 8 GPUs each and cordons one. It submits a job of 3 pods that
each need a whole node, waits 15 seconds and checks that no pod is running. It then uncordons the
third node and checks that every pod runs within a minute. A scheduler that places pods one at a
time runs 2 of them in the first step and fails.
