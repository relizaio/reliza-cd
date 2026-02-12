# Reliza CD

Reliza CD is a tool that acts as an agent on the Kubernetes side to connect the instance to [Reliza Hub](https://relizahub.com). The deployments to the instance may be then controlled from Reliza Hub.

The recommended way to install is to use [Reliza CD Helm Chart](https://github.com/relizaio/helm-charts#3-reliza-cd-helm-chart).

## Dry Run Mode

To enable dry run mode, set the `DRY_RUN` environment variable to `true`:

```
DRY_RUN=true
```

In this mode, Reliza CD will log all mutating helm and kubectl commands (install, upgrade, uninstall, delete, create namespace) but will not execute them. Read-only operations such as chart downloads, value merging, and metadata streaming will continue to run normally.