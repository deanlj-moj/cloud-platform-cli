## cloud-platform environment bump-module

Bump all specified module versions

```
cloud-platform environment bump-module [flags]
```

### Examples

```
cloud-platform environments bump-module --module serviceaccount --module-version 1.1.1

Would bump all users serviceaccount modules in the environments repository to the specified version.
	
```

### Options

```
  -h, --help                    help for bump-module
  -m, --module string           Module to upgrade the version
  -v, --module-version string   Semantic version to bump a module to
```

### Options inherited from parent commands

```
      --skip-version-check   don't check for updates
```

### SEE ALSO

* [cloud-platform environment](cloud-platform_environment.md)	 - Cloud Platform Environment actions

