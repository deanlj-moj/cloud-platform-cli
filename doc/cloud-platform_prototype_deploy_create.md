## cloud-platform prototype deploy create

Create the deployment files and github actions required to deploy in Cloud Platform

### Synopsis


	Create the deployment files and github actions required to deploy the Prototype kit from a github repository in Cloud Platform.

The files will be generated based on where the current local branch of the prototype github repository is pointed to:

  https://[namespace name]-[branch-name].apps.live.cloud-platform.service.justice.gov.uk

A continuous deployment workflow will be created in the github repository such
that any changes to the branch are deployed to the cloud platform.
	

```
cloud-platform prototype deploy create [flags]
```

### Examples

```
> cloud-platform prototype deploy

```

### Options

```
  -h, --help                help for create
  -s, --skip-docker-files   Whether to skip the files required to build the docker image i.e Dockerfile, .dockerignore, start.sh
```

### Options inherited from parent commands

```
      --skip-version-check   don't check for updates
```

### SEE ALSO

* [cloud-platform prototype deploy](cloud-platform_prototype_deploy.md)	 - Cloud Platform Environment actions

