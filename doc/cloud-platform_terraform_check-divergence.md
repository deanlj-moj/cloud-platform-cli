## cloud-platform terraform check-divergence

Terraform check-divergence check if there are drifts in the state.

```
cloud-platform terraform check-divergence [flags]
```

### Options

```
      --aws-access-key-id string       Access key id of service account to be used by terraform
      --aws-secret-access-key string   Secret access key of service account to be used by terraform
      --dirs-file string               Required for bulk-plans, file path which holds directories where terraform plan is going to be executed
  -d, --display-tf-output              Display or not terraform plan output (default true)
  -h, --help                           help for check-divergence
  -v, --var-file string                tfvar to be used by terraform
  -w, --workspace string               Default workspace where terraform is going to be executed (default "default")
```

### Options inherited from parent commands

```
      --skip-version-check   don't check for updates
```

### SEE ALSO

* [cloud-platform terraform](cloud-platform_terraform.md)	 - Terraform actions.

