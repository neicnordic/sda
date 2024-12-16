# linter modules to be included/excluded
LINT_INCLUDE=-E bodyclose,gocritic,gofmt,gosec,govet,nestif,nlreturn,revive,rowserrcheck
LINT_EXCLUDE=-e G401,G501,G107

help:
	@echo 'Welcome!'
	@echo ''
	@echo 'This Makefile is designed to make the development work go smoothly.'
	@echo 'In-depth description of how to use this Makefile can be found in the README.md'

bootstrap: go-version-check docker-version-check
		@for dir in sda sda-download; do \
			cd $$dir; \
			go get ./...; \
			cd ..; \
		done
		@if ! command -v curl >/dev/null; then \
			echo "Can't install golangci-lint because curl is missing."; \
			exit 1; \
		fi
		@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
		sh -s -- -b $$(go env GOPATH)/bin
		go install golang.org/x/tools/cmd/goimports@latest

# build containers
build-all: build-postgresql build-rabbitmq build-sda build-sda-download build-sda-sftp-inbox build-sda-admin
build-postgresql:
	@cd postgresql && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-postgres .
build-rabbitmq:
	@cd rabbitmq && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-rabbitmq .
build-sda:
	@cd sda && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F) .
build-sda-download:
	@cd sda-download && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-download .
build-sda-sftp-inbox:
	@cd sda-sftp-inbox && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-sftp-inbox .
build-sda-admin:
	@cd sda-admin && go build


go-version-check: SHELL:=/bin/bash
go-version-check:
	@GO_VERSION_MIN=$$(grep GOLANG_VERSION $(CURDIR)/sda/Dockerfile | cut -d '-' -f2 | tr -d '}'); \
	GO_VERSION=$$(go version | grep -o 'go[0-9]\+\.[0-9]\+\(\.[0-9]\+\)\?' | tr -d 'go'); \
	IFS="." read -r -a GO_VERSION_ARR <<< "$${GO_VERSION}"; \
	IFS="." read -r -a GO_VERSION_REQ <<< "$${GO_VERSION_MIN}"; \
	if [[ $${GO_VERSION_ARR[0]} -lt $${GO_VERSION_REQ[0]} ||\
		( $${GO_VERSION_ARR[0]} -eq $${GO_VERSION_REQ[0]} &&\
		( $${GO_VERSION_ARR[1]} -lt $${GO_VERSION_REQ[1]} ||\
		( $${GO_VERSION_ARR[1]} -eq $${GO_VERSION_REQ[1]} && $${GO_VERSION_ARR[2]} -lt $${GO_VERSION_REQ[2]} )))\
	]]; then\
		echo "SDA requires go $${GO_VERSION_MIN} to build; found $${GO_VERSION}."; \
		exit 1; \
	fi; \
	echo "GO version: $${GO_VERSION}."

docker-version-check:
	@DOCKER_VERSION=$$(docker version -f "{{.Server.Version}}" | cut -d'.' -f 1); \
	DOCKER_COMPOSE_VERSION=$$(docker compose version | cut -d'v' -f 3 | cut -d'.' -f 1); \
	if [ $${DOCKER_VERSION} -lt 24 ]; then \
		echo "Docker version less than 24 can't continue"; \
		exit 1;\
	fi; \
	if [ $${DOCKER_COMPOSE_VERSION} -lt 2 ]; then \
		echo "Docker compose version less than 2 can't continue"; \
		exit 1;\
	fi; \
	if [ ! $$(docker buildx version | cut -d' ' -f 2) ]; then \
		echo "Docker buildx does not exist can't continue"; \
		exit 1;\
	fi; \
	echo "Docker version: $${DOCKER_VERSION}."; \
	echo "Docker Compose version: $${DOCKER_COMPOSE_VERSION}."

# bring up the services
sda-s3-up:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml up -d
sda-posix-up:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml up -d
sda-sync-up:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml up -d

# bring down the services
sda-s3-down:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml down -v --remove-orphans
sda-posix-down:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml down -v --remove-orphans
sda-sync-down:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml down -v --remove-orphans

# run intrgration tests, same as being run in Github Actions during a PR
integrationtest-postgres: build-postgresql
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/postgres.yml run tests
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/postgres.yml down -v --remove-orphans
integrationtest-rabbitmq: build-rabbitmq build-sda
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/rabbitmq-federation.yml run federation_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/rabbitmq-federation.yml down -v --remove-orphans

integrationtest-sda: build-all
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml run integration_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml down -v --remove-orphans
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml run integration_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml down -v --remove-orphans
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml run integration_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml down -v --remove-orphans

integrationtest-sda-posix: build-all
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml run integration_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml down -v --remove-orphans
integrationtest-sda-posix-run:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml run integration_test
integrationtest-sda-posix-down:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml down -v --remove-orphans

integrationtest-sda-s3: build-all
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml run integration_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml down -v --remove-orphans
integrationtest-sda-s3-run:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml run integration_test
integrationtest-sda-s3-down:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml down -v --remove-orphans

integrationtest-sda-sync: build-all
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml run integration_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml down -v --remove-orphans
integrationtest-sda-sync-run:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml run integration_test
integrationtest-sda-sync-down:
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-sync-integration.yml down -v --remove-orphans

# lint go code
lint-all: lint-sda lint-sda-download lint-sda-admin
lint-sda:
	@echo 'Running golangci-lint in the `sda` folder'
	@cd sda && golangci-lint run $(LINT_INCLUDE) $(LINT_EXCLUDE)
lint-sda-download:
	@echo 'Running golangci-lint in the `sda-download` folder'
	@cd sda-download && golangci-lint run $(LINT_INCLUDE) $(LINT_EXCLUDE)
lint-sda-admin:
	@echo 'Running golangci-lint in the `sda-admin` folder'
	@cd sda-admin && golangci-lint run $(LINT_INCLUDE) $(LINT_EXCLUDE)

# run static code tests
test-all: test-sda test-sda-download test-sda-sftp-inbox
test-sda:
	@cd sda && go test ./... -count=1
test-sda-download:
	@cd sda-download && go test ./... -count=1
test-sda-sftp-inbox:
	@docker run --rm -v ./sda-sftp-inbox:/inbox maven:3.9.4-eclipse-temurin-21-alpine sh -c "cd /inbox && mvn test -B"
test-sda-admin:
	@cd sda-admin && go test ./... -count=1

# local k8s with k3d
k3d-version-check: SHELL:=/bin/bash
k3d-version-check:
	@K3D_VERSION=$$(k3d version | cut -d'v' -f 3 | head -n 1); \
	IFS="." read -r -a K3D_VERSION_ARR <<< "$${K3D_VERSION}"; \
	IFS="." read -r -a K3D_VERSION_REQ <<< 5.7.0; \
	if [[ $${K3D_VERSION_ARR[0]} -lt $${K3D_VERSION_REQ[0]} ||\
		( $${K3D_VERSION_ARR[0]} -eq $${K3D_VERSION_REQ[0]} &&\
		( $${K3D_VERSION_ARR[1]} -lt $${K3D_VERSION_REQ[1]} ||\
		( $${K3D_VERSION_ARR[1]} -eq $${K3D_VERSION_REQ[1]} && $${K3D_VERSION_ARR[2]} -lt $${K3D_VERSION_REQ[2]} )))\
	]]; then\
		echo "Helm chart testing requires k3d $${K3D_VERSION_MIN}.";\
		exit 1;\
	fi; \
	echo "detected k3d version: $${K3D_VERSION}"
	@if [ ! $$(command -v kubectl) ]; then\
		echo "kubectl is missing";\
	fi
k3d-create-cluster:
	@k3d cluster create --k3s-arg "--disable=traefik@server:0" --port "80:80@loadbalancer" --port "443:443@loadbalancer"; \
	helm upgrade --install ingress-nginx ingress-nginx --repo https://kubernetes.github.io/ingress-nginx --namespace ingress-nginx --create-namespace
k3d-delete-cluster:
	@k3d cluster delete
k3d-deploy-dependencies:
	@bash .github/integration/scripts/charts/dependencies.sh local
k3d-import-images: build-all
	@bash .github/integration/scripts/charts/import_local_images.sh k3s-default
k3d-deploy-postgres:
	@bash .github/integration/scripts/charts/deploy_charts.sh sda-db "$$(date +%F)" false
k3d-deploy-rabbitmq:
	@bash .github/integration/scripts/charts/deploy_charts.sh sda-mq "$$(date +%F)" false
k3d-deploy-sda-s3:
	@bash .github/integration/scripts/charts/deploy_charts.sh sda-svc "$$(date +%F)" false s3
k3d-deploy-sda-posix:
	@bash .github/integration/scripts/charts/deploy_charts.sh sda-svc "$$(date +%F)" false posix
k3d-cleanup-all-deployments:
	@bash .github/integration/scripts/charts/cleanup.sh