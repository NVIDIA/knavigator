## Example of running `Run:ai` with `knavigator`

The support for Run:ai in Knavigator is currently experimental. To utilize this feature, a valid subscription with Run:ai is required.

1. **Create a new project**

Navigate to the Run:ai portal and create a new project. Upon creating the project, the portal will provide Helm instructions for deploying the Run:ai cluster. These instructions will include:
  - `controlPlane.url`
  - `controlPlane.clientSecret`
  - `cluster.uid`

:warning: **Note:** Do not execute the provided Helm command directly. Instead, follow the steps below.

2. **Define Environment Variables**:

  - `RUNAI_CONTROL_PLANE_URL`: Set this to the `controlPlane.url` provided.
  - `RUNAI_CLIENT_SECRET`: Set this to the `controlPlane.clientSecret` provided.
  - `RUNAI_CLUSTER_ID`: Set this to the `cluster.uid` provided.

3. **Run the Deployment Script**:

  Execute the [create-test-cluster.sh](../../../scripts/create-test-cluster.sh) script to complete the deployment.

This script will deploy a `kind` cluster if necessary, followed by deploying `KWOK` and `Prometheus`. It will then prompt you to select a workload manager. Choose the `run:ai` option.


4. **Replace cluster UID and project name in the sample workflow files**:

Update the sample workflow files [test-trainingworkload.yml](../../../resources/workflows/runai/test-trainingworkload.yml#L40-L41) and [test-distributedworkload.yml](../../../resources/workflows/runai/test-distributedworkload.yml#L40-L41) by replacing `<RUNAI_CLUSTER_ID>` with the cluster UID and `<RUNAI_PROJECT>` with the project name.

5. **Run the workflows**

Run a Run:ai training workload: 
```bash
./bin/knavigator -workflow resources/workflows/runai/test-trainingworkload.yml -cleanup
```

Run a Run:ai distributed workload: 
```bash
./bin/knavigator -workflow resources/workflows/runai/test-distributedworkload.yml -cleanup
```
