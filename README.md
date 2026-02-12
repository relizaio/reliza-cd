# Reliza CD

Reliza CD is a tool that acts as an agent on the Kubernetes side to connect the instance to [Reliza Hub](https://relizahub.com). The deployments to the instance may be then controlled from Reliza Hub.

The recommended way to install is to use [Reliza CD Helm Chart](https://github.com/relizaio/helm-charts#3-reliza-cd-helm-chart).

## Dry Run Mode

To enable dry run mode, set the `DRY_RUN` environment variable to `true`:

```
DRY_RUN=true
```

In this mode, Reliza CD will log all mutating helm and kubectl commands (install, upgrade, uninstall, delete, create namespace) but will not execute them. Read-only operations such as chart downloads, value merging, and metadata streaming will continue to run normally.

## Debug Logging

To enable debug level logging, set the `LOG_LEVEL` environment variable to `debug`:

```
LOG_LEVEL=debug
```

This will output additional diagnostic information such as custom values resolution details and other internal state.

## Workspace Backup to S3

Reliza CD can periodically back up the workspace directory to an S3 bucket. Backups are encrypted with AES-256-CBC before upload.

To enable, set the following environment variables:

| Variable | Required | Description |
|---|---|---|
| `BACKUP_ENABLED` | Yes | Set to `true` to enable backups |
| `BACKUP_SCHEDULE` | Yes | Cron schedule expression (e.g. `0 2 * * *` for daily at 2 AM) |
| `AWS_REGION` | Yes | AWS region of the S3 bucket |
| `AWS_BUCKET` | Yes | S3 bucket name |
| `ENCRYPTION_PASSWORD` | Yes | Password used for AES-256-CBC encryption |
| `AWS_ACCESS_KEY_ID` | No | AWS access key (falls back to default AWS credential chain) |
| `AWS_SECRET_ACCESS_KEY` | No | AWS secret key (falls back to default AWS credential chain) |
| `BACKUP_PREFIX` | No | Prefix for backup file names in S3 |

The backup process:
1. Creates a tar.gz archive of the workspace directory
2. Encrypts it using `openssl enc -aes-256-cbc -a -pbkdf2 -iter 600000 -salt`
3. Uploads the encrypted file to the specified S3 bucket