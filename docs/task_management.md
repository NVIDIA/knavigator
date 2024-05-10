# Task Management

Tasks are defined in a YAML format or received via HTTP/gRPC protocols. The Task Manager parses these tasks and executes them using the Kubernetes API. Supported tasks include:

- Create/configure/modify virtual nodes
- Create/modify/delete custom resource objects
- Check the spec and status of custom resource objects
- Check the spec and status of nodes
- Check the spec and status of pods
- Run PromQL query
- Sleep for a specified duration
