# Sensitive Data Archive

The `SDA` contains all components of [NeIC Sensitive Data Archive](https://neic-sda.readthedocs.io/en/latest/). It can be used as part of a [Federated EGA](https://ega-archive.org/federated) or as a standalone Sensitive Data Archive.

For more information about the different components, please refer to the README files in their respective folders.

## How to run the SDA stack 
The following instructions outline the steps to set up and run the `SDA` services for development and testing using Docker. These steps are based on the provided [Makefile](./Makefile) commands.

### Prerequisites
Ensure you have the following installed on your system:

- [`Go`](https://www.golang.org/): The required version is specified in the `sda` Dockerfile. Verify using
    ```sh
    $ make go-version-check
    ```

- Docker: Version 24 or higher. Verify using 
    ```sh
    $ make docker-version-check 
    ```
- Docker Compose: Version 2 or higher. For Linux, ensure the [Compose plugin](https://docs.docker.com/compose/install/linux/) is installed.

In preparation for local development, it is essential to verify that `$GOPATH/bin` is part of the system PATH, as certain distributions may package outdated versions of build tools. SDA uses [Go Modules](https://github.com/golang/go/wiki/Modules), and it is advisable to clone the repository outside the `GOPATH`. After cloning, initialize the environment and obtain necessary build tools using the bootstrap command: 

```sh
$ make bootstrap
```

### Build Docker images 

Build the required Docker images for all SDA services:

```sh
$ make build-all
```

You can also build images for individual services by replacing `all` with the folder name (`postgresql`, `rabbitmq`, `sda`, `sda-download`, `sda-sftp-inbox`), for example

```sh
$ make build-sda
```

To build the `sda-admin` CLI tool:

```sh
$ make build-sda-admin
```

### Running the services

#### Start services with Docker Compose
The following command will bring up all services using the Docker Compose file [sda-s3-integration.yml](.github/integration/sda-s3-integration.yml) (configured for S3 as the storage backend):

```sh
$ make sda-s3-up
```

#### Shut down all services and clean up resources
The following command will shut down all services and clean up all related resources:

```sh
$ make sda-s3-down
```

For the setup with POSIX as the storage backend, use 
`make sda-posix-up` and `make sda-posix-down` to start and shut down services. 

For the setup including the [`sync`](https://github.com/neicnordic/sda-sync) service, use `make sda-sync-up` and `make sda-sync-down` to start and shut down services.

### Running the integration tests
This will build all required images, bring up the services, run the integration test, and then shut down services and clean up resources. The same test runs on every pull request (PR) in GitHub.

- Integration test for the database:
    ```sh
    make integrationtest-postgres
    ```
- Integration test for RabbitMQ:
    ```sh
    make integrationtest-rabbitmq
    ```
- Integration test for all SDA setups (including S3, POSIX and sync):
    ```sh
    make integrationtest-sda
    ```
- Integration test for SDA using POSIX as the storage backend:
    ```sh
    make integrationtest-sda-posix
    ```
- Integration test for SDA using S3 as the storage backend:
    ```sh
    make integrationtest-sda-s3
    ```
- Integration test for SDA including the sync service:
    ```sh
    make integrationtest-sda-sync
    ```
#### Running the integration tests without shutting down the services 
This will run the integration tests and keep the services running after the tests are finished.

- Integration test for SDA using POSIX as the storage backend:
    ```sh
    make integrationtest-sda-posix-run
    ```
- Integration test for SDA using S3 as the storage backend:
    ```sh
    make integrationtest-sda-s3-run
    ```
- Integration test for SDA including the sync service:
    ```sh
    make integrationtest-sda-sync-run
    ```

After that, you will need to shut down the services manually.

- Shut down services for SDA using POSIX as the storage backend
    ```sh
    make integrationtest-sda-posix-down
    ```
- Shut down services for SDA using S3 as the storage backend
    ```sh
    make integrationtest-sda-s3-down
    ```
- Shut down services for SDA including the sync service:
    ```sh
    make integrationtest-sda-sync-down
    ```

### Linting the Go code

To run `golangci-lint` for all Go components:

```sh
$ make lint-all
```

To run `golangci-lint` for a specific component, replace `all` with the folder name (`sda`, `sda-auth`, `sda-download`), for example:

```sh
$ make lint-sda
```

### Running the static code tests

For Go code, this means running `go test -count=1 ./...` in the target folder. For the *sftp-inbox* this calls `mvn test -B` inside a Maven container.

To run the static code tests for all components:

```sh
$ make test-all
```

To run the static code tests for a specific component, replace `all` with the folder name (`sda`, `sda-admin`, `sda-download`, `sda-sftp-inbox`), for example:

```sh
$ make test-sda
```

## Testing and developing the helm charts locally

Developing and testing the Helm charts (or other deployment manifests) requires a Kubernetes environment. One of the most lightweight distributions available is [k3d](https://k3d.io/stable/).

### install k3d

The simplest way to install k3d is by using the official install script.

- wget:

```bash
wget -q -O - https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
```

- curl:

```bash
curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
```

#### Create a cluster

Once installed a cluster named `test-cluster` can be created as such:

```sh
k3d cluster create test-cluster
```

Or by using the `make k3d-create-cluster` command, you can create a cluster named `k3s-default`.

The new cluster's connection details will automatically be merged into your default kubeconfig and activated. The command below should show the created node.

```sh
kubectl get nodes
```

The Nginx ingress controller is deployed and will bind to port 80 and 443 of the host system. As such a deployed service with an ingress definition can then be targeted by setting the `Host: HOSTNAME` header fo that service.

```sh
curl -H "Host: test" http://localhost/
```

For testing ingress endpoints with other applications like a web browser, the hosts file `/etc/hosts` will ned to be edited unless some other form of DNS modifivcaion is used.

#### Remove the cluster

Removing the cluster can be done using the `make k3d-delete-cluster` command or as shown below if a specific name is used during creation.

```sh
k3d cluster delete test-cluster
```

#### Install kubectl

If `kubectl` is not installed, run the following command to download the latest stable version. (substitue `linux/amd64` with `darwin/arm64` if you are using a Mac).

```sh
curl -sLO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
```

### Deploy the components

Deployment of the charts can be done as describe below in more detail, or by using the corresponding command in the [Makefile](./Makefile)

#### Makefile commands

- make k3d-deploy-dependencies - bootstrap dependencies
- make k3d-import-images - build and import images into the default cluster named `k3s-default`
- make k3d-deploy-postgres - deploy the sda-db chart without TLS
- make k3d-deploy-rabbitmq - deploy the sda-mq chart without TLS
- make k3d-deploy-sda-s3 - deploy the sda-svc chart with S3 storage without TLS
- make k3d-deploy-sda-posix - deploy the sda-svc chart with POSIX storage without TLS
- make k3d-cleanup-all-deployments - Remove all deployed components and dependencies

#### Bootstrap the dependencies

This script requires [yq](https://github.com/mikefarah/yq/releases/latest), the GO version of [crypt4gh](https://github.com/neicnordic/crypt4gh/releases/latest) as well as [xxd](https://manpages.org/xxd) and [jq](https://manpages.org/jq) to be installed.

```sh
bash .github/integration/scripts/charts/dependencies.sh local
```

#### Deploy the Sensitive Data Archive components

Start by building the required containers using the `make build-all` command, once that has completed the images can be imported to the cluster.

```sh
bash .github/integration/scripts/charts/import_local_images.sh <CLUTER_NAME>
```

The Postgres and RabbitMQ Needs to be deployed first, the bool at the end specifies if TLS should be enabled or not for the deployes services.  
Replace `sda-db` in the example below with the helmc hart that shuld be installed. (`sda-db` or `sda-mq`)

```sh
bash .github/integration/scripts/charts/deploy_charts.sh sda-db "$(date +%F)" false
```

Once the DB and MQ are installed the SDA stack can be installed, here the desired storage backend needs to specified as well (`posix` or `s3`)

```sh
bash .github/integration/scripts/charts/deploy_charts.sh sda-svc "$(date +%F)" false s3
```

#### Cleanup all deployed components

Once the testing is concluded all deployed components can be removed.

```sh
bash .github/integration/scripts/charts/cleanup.sh
```
