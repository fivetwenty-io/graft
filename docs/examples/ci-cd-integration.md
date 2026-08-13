# CI/CD Integration

This guide demonstrates how to integrate Graft into continuous integration and deployment pipelines.

## GitHub Actions

### Basic Configuration Generation

**.github/workflows/config.yml:**

```yaml
name: Generate Configuration

on:
  push:
    branches: [main]
    paths:
      - 'config/**'
  pull_request:
    paths:
      - 'config/**'

jobs:
  generate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Graft
        run: |
          GRAFT_VERSION=1.31.1
          curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz
          sudo mv graft /usr/local/bin/

      - name: Generate Configurations
        run: |
          mkdir -p generated
          for env in development staging production; do
            graft merge \
              config/base.yml \
              config/environments/${env}.yml \
              > generated/${env}-config.yml
          done

      - name: Upload Artifacts
        uses: actions/upload-artifact@v4
        with:
          name: configurations
          path: generated/
```

### Configuration Validation

**.github/workflows/validate.yml:**

```yaml
name: Validate Configuration

on:
  pull_request:
    paths:
      - 'config/**'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Graft
        run: |
          GRAFT_VERSION=1.31.1
          curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz
          sudo mv graft /usr/local/bin/

      - name: Validate Base Configuration
        run: |
          graft merge config/base.yml > /dev/null
          echo "Base configuration is valid"

      - name: Validate All Environments
        run: |
          for env in development staging production; do
            echo "Validating ${env}..."
            graft merge \
              config/base.yml \
              config/environments/${env}.yml \
              > /dev/null
            echo "${env} configuration is valid"
          done

      - name: Check for Required Parameters
        run: |
          # This will fail if any (( param )) is unresolved
          graft merge \
            config/base.yml \
            config/environments/production.yml \
            config/secrets/production.yml \
            --skip-eval \
            > /dev/null
```

### With Vault Secrets

**.github/workflows/deploy.yml:**

```yaml
name: Deploy with Secrets

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - uses: actions/checkout@v4

      - name: Install Graft
        run: |
          GRAFT_VERSION=1.31.1
          curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz
          sudo mv graft /usr/local/bin/

      - name: Import Secrets from Vault
        uses: hashicorp/vault-action@v2
        with:
          url: ${{ secrets.VAULT_ADDR }}
          token: ${{ secrets.VAULT_TOKEN }}
          secrets: |
            secret/data/production/database password | DB_PASSWORD ;
            secret/data/production/api key | API_KEY

      - name: Generate Production Config
        env:
          VAULT_ADDR: ${{ secrets.VAULT_ADDR }}
          VAULT_TOKEN: ${{ secrets.VAULT_TOKEN }}
        run: |
          graft merge \
            config/base.yml \
            config/environments/production.yml \
            config/secrets/production.yml \
            > production-config.yml

      - name: Deploy
        run: |
          # Deploy using generated configuration
          kubectl apply -f production-config.yml
```

### Pull Request Diff

**.github/workflows/pr-diff.yml:**

```yaml
name: Configuration Diff

on:
  pull_request:
    paths:
      - 'config/**'

jobs:
  diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Install Graft
        run: |
          GRAFT_VERSION=1.31.1
          curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz
          sudo mv graft /usr/local/bin/

      - name: Generate Current Config
        run: |
          graft merge \
            config/base.yml \
            config/environments/production.yml \
            > /tmp/current.yml

      - name: Generate Base Config
        run: |
          git checkout ${{ github.event.pull_request.base.sha }}
          graft merge \
            config/base.yml \
            config/environments/production.yml \
            > /tmp/base.yml
          git checkout ${{ github.sha }}

      - name: Show Diff
        run: |
          echo "## Configuration Changes" >> $GITHUB_STEP_SUMMARY
          echo '```' >> $GITHUB_STEP_SUMMARY
          graft diff --changes /tmp/base.yml /tmp/current.yml >> $GITHUB_STEP_SUMMARY || true
          echo '```' >> $GITHUB_STEP_SUMMARY
```

## GitLab CI

### Basic Pipeline

**.gitlab-ci.yml:**

```yaml
stages:
  - validate
  - build
  - deploy

variables:
  GRAFT_VERSION: "1.31.0"

.install_graft: &install_graft
  before_script:
    - curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz -C /usr/local/bin

validate:
  stage: validate
  <<: *install_graft
  script:
    - |
      for env in development staging production; do
        echo "Validating ${env}..."
        graft merge config/base.yml config/environments/${env}.yml > /dev/null
      done
  rules:
    - changes:
        - config/**/*

build:configs:
  stage: build
  <<: *install_graft
  script:
    - mkdir -p generated
    - |
      for env in development staging production; do
        graft merge \
          config/base.yml \
          config/environments/${env}.yml \
          > generated/${env}-config.yml
      done
  artifacts:
    paths:
      - generated/
    expire_in: 1 week
  rules:
    - if: $CI_COMMIT_BRANCH == "main"

deploy:staging:
  stage: deploy
  <<: *install_graft
  environment:
    name: staging
  script:
    - |
      graft merge \
        config/base.yml \
        config/environments/staging.yml \
        config/secrets/staging.yml \
        > staging-config.yml
    - kubectl apply -f staging-config.yml
  rules:
    - if: $CI_COMMIT_BRANCH == "main"

deploy:production:
  stage: deploy
  <<: *install_graft
  environment:
    name: production
  script:
    - |
      graft merge \
        config/base.yml \
        config/environments/production.yml \
        config/secrets/production.yml \
        > production-config.yml
    - kubectl apply -f production-config.yml
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
  when: manual
```

### With Vault Integration

**.gitlab-ci.yml:**

```yaml
variables:
  VAULT_ADDR: https://vault.example.com

deploy:production:
  stage: deploy
  image: alpine:latest
  id_tokens:
    VAULT_ID_TOKEN:
      aud: https://vault.example.com
  before_script:
    # Install graft
    - apk add --no-cache curl
    - GRAFT_VERSION=1.31.1
    - curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz -C /usr/local/bin
    # Authenticate to Vault using JWT
    - |
      export VAULT_TOKEN=$(curl -s \
        --request POST \
        --data "{\"jwt\": \"${VAULT_ID_TOKEN}\", \"role\": \"gitlab-ci\"}" \
        ${VAULT_ADDR}/v1/auth/jwt/login | jq -r '.auth.client_token')
  script:
    - |
      graft merge \
        config/base.yml \
        config/environments/production.yml \
        config/secrets/production.yml \
        > production-config.yml
    - kubectl apply -f production-config.yml
  environment:
    name: production
```

## Jenkins

### Jenkinsfile

```groovy
pipeline {
    agent any

    environment {
        VAULT_ADDR = credentials('vault-addr')
        VAULT_TOKEN = credentials('vault-token')
    }

    stages {
        stage('Install Graft') {
            steps {
                sh '''
                    GRAFT_VERSION=1.31.1
                    curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz
                    mv graft /usr/local/bin/
                '''
            }
        }

        stage('Validate') {
            steps {
                sh '''
                    for env in development staging production; do
                        echo "Validating ${env}..."
                        graft merge config/base.yml config/environments/${env}.yml > /dev/null
                    done
                '''
            }
        }

        stage('Generate Configs') {
            steps {
                sh '''
                    mkdir -p generated
                    for env in development staging production; do
                        graft merge \
                            config/base.yml \
                            config/environments/${env}.yml \
                            > generated/${env}-config.yml
                    done
                '''
            }
        }

        stage('Deploy to Staging') {
            when {
                branch 'main'
            }
            steps {
                sh '''
                    graft merge \
                        config/base.yml \
                        config/environments/staging.yml \
                        config/secrets/staging.yml \
                        > staging-config.yml
                    kubectl apply -f staging-config.yml
                '''
            }
        }

        stage('Deploy to Production') {
            when {
                branch 'main'
            }
            input {
                message "Deploy to production?"
                ok "Deploy"
            }
            steps {
                sh '''
                    graft merge \
                        config/base.yml \
                        config/environments/production.yml \
                        config/secrets/production.yml \
                        > production-config.yml
                    kubectl apply -f production-config.yml
                '''
            }
        }
    }

    post {
        always {
            archiveArtifacts artifacts: 'generated/*.yml', fingerprint: true
        }
    }
}
```

## CircleCI

### config.yml

**.circleci/config.yml:**

```yaml
version: 2.1

executors:
  graft:
    docker:
      - image: alpine:latest

commands:
  install-graft:
    steps:
      - run:
          name: Install Graft
          command: |
            apk add --no-cache curl
            GRAFT_VERSION=1.31.1
            curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz -C /usr/local/bin

jobs:
  validate:
    executor: graft
    steps:
      - checkout
      - install-graft
      - run:
          name: Validate Configurations
          command: |
            for env in development staging production; do
              echo "Validating ${env}..."
              graft merge config/base.yml config/environments/${env}.yml > /dev/null
            done

  build:
    executor: graft
    steps:
      - checkout
      - install-graft
      - run:
          name: Generate Configurations
          command: |
            mkdir -p generated
            for env in development staging production; do
              graft merge \
                config/base.yml \
                config/environments/${env}.yml \
                > generated/${env}-config.yml
            done
      - persist_to_workspace:
          root: .
          paths:
            - generated

  deploy-staging:
    executor: graft
    steps:
      - checkout
      - install-graft
      - run:
          name: Deploy to Staging
          command: |
            graft merge \
              config/base.yml \
              config/environments/staging.yml \
              config/secrets/staging.yml \
              > staging-config.yml
            # kubectl apply -f staging-config.yml

  deploy-production:
    executor: graft
    steps:
      - checkout
      - install-graft
      - run:
          name: Deploy to Production
          command: |
            graft merge \
              config/base.yml \
              config/environments/production.yml \
              config/secrets/production.yml \
              > production-config.yml
            # kubectl apply -f production-config.yml

workflows:
  version: 2
  build-and-deploy:
    jobs:
      - validate
      - build:
          requires:
            - validate
      - deploy-staging:
          requires:
            - build
          filters:
            branches:
              only: main
      - hold:
          type: approval
          requires:
            - deploy-staging
      - deploy-production:
          requires:
            - hold
```

## Dynamic Configuration Generation

### Environment Variable Injection

**config/base.yml:**

```yaml
meta:
  app_name: (( grab $APP_NAME || "my-app" ))
  version: (( grab $APP_VERSION || "latest" ))
  environment: (( grab $ENVIRONMENT || "development" ))
  commit_sha: (( grab $CI_COMMIT_SHA || "unknown" ))
  build_number: (( grab $CI_BUILD_NUMBER || "local" ))

image:
  repository: (( grab $IMAGE_REPOSITORY || "myregistry/my-app" ))
  tag: (( concat meta.version "-" meta.commit_sha ))

labels:
  app: (( grab meta.app_name ))
  version: (( grab meta.version ))
  environment: (( grab meta.environment ))
  commit: (( grab meta.commit_sha ))
  build: (( grab meta.build_number ))
```

**CI Usage:**

```sh
export APP_VERSION="2.5.0"
export ENVIRONMENT="production"
export CI_COMMIT_SHA="${GITHUB_SHA:0:7}"
export CI_BUILD_NUMBER="${GITHUB_RUN_NUMBER}"

graft merge config/base.yml config/environments/production.yml
```

### Git-Based Configuration

**config/git-info.yml:**

```yaml
git:
  branch: (( grab $GIT_BRANCH || "unknown" ))
  commit: (( grab $GIT_COMMIT || "unknown" ))
  tag: (( grab $GIT_TAG || "" ))
  author: (( grab $GIT_AUTHOR || "unknown" ))

deployment:
  (( if git.tag != "" ))
  version: (( grab git.tag ))
  (( else ))
  version: (( concat git.branch "-" git.commit ))
  (( fi ))
```

**CI Script:**

```sh
export GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
export GIT_COMMIT=$(git rev-parse --short HEAD)
export GIT_TAG=$(git describe --tags --exact-match 2>/dev/null || echo "")
export GIT_AUTHOR=$(git log -1 --format='%an')

graft merge config/base.yml config/git-info.yml
```

## Validation in Pipelines

### Schema Validation

**validate.sh:**

```bash
#!/bin/bash
set -e

ENVIRONMENTS="development staging production"

for env in $ENVIRONMENTS; do
  echo "Validating ${env} configuration..."

  # Generate configuration
  graft merge \
    config/base.yml \
    config/environments/${env}.yml \
    > /tmp/${env}-config.yml

  # Check required fields exist
  required_fields=(
    "database.host"
    "database.port"
    "server.port"
  )

  for field in "${required_fields[@]}"; do
    if ! graft merge /tmp/${env}-config.yml --cherry-pick ${field} > /dev/null 2>&1; then
      echo "ERROR: Missing required field '${field}' in ${env}"
      exit 1
    fi
  done

  echo "${env} configuration is valid"
done

echo "All configurations validated successfully"
```

### Diff-Based Change Detection

**detect-changes.sh:**

```bash
#!/bin/bash
set -e

# Get the base branch configuration
git checkout origin/main -- config/
graft merge config/base.yml config/environments/production.yml > /tmp/main-config.yml
git checkout -- config/

# Get the current branch configuration
graft merge config/base.yml config/environments/production.yml > /tmp/current-config.yml

# Check for changes. graft diff exits 0 when the documents are
# semantically identical, 1 when they differ, and 2 on an error, so the
# exit code alone answers the question -- no need to test the output file.
set +e
graft diff --changes /tmp/main-config.yml /tmp/current-config.yml > /tmp/changes.txt
status=$?
set -e

case "$status" in
  0)
    echo "No configuration changes"
    exit 0
    ;;
  1)
    echo "Configuration changes detected:"
    cat /tmp/changes.txt
    exit 0
    ;;
  *)
    echo "Error comparing configurations"
    exit 1
    ;;
esac
```

## Kubernetes Deployment

### Complete Kubernetes Workflow

**.github/workflows/k8s-deploy.yml:**

```yaml
name: Kubernetes Deployment

on:
  push:
    branches: [main]
    paths:
      - 'config/**'
      - 'k8s/**'

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Graft
        run: |
          GRAFT_VERSION=1.31.1
          curl -sSL https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz
          sudo mv graft /usr/local/bin/

      - name: Configure kubectl
        uses: azure/k8s-set-context@v3
        with:
          kubeconfig: ${{ secrets.KUBE_CONFIG }}

      - name: Generate ConfigMap
        run: |
          # Generate application config
          graft merge \
            config/base.yml \
            config/environments/production.yml \
            > /tmp/app-config.yml

          # Create ConfigMap manifest
          cat > k8s/configmap.yml << EOF
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: app-config
            namespace: production
          data:
            config.yml: |
          $(cat /tmp/app-config.yml | sed 's/^/      /')
          EOF

      - name: Generate Deployment
        env:
          IMAGE_TAG: ${{ github.sha }}
        run: |
          graft merge \
            k8s/templates/deployment.yml \
            config/environments/production.yml \
            > k8s/deployment.yml

      - name: Apply Kubernetes Manifests
        run: |
          kubectl apply -f k8s/configmap.yml
          kubectl apply -f k8s/deployment.yml

      - name: Wait for Rollout
        run: |
          kubectl rollout status deployment/my-app -n production --timeout=300s
```

### Helm Values Generation

**Generate Helm values from Graft configuration:**

```sh
# Generate values for Helm
graft merge \
  config/base.yml \
  config/environments/production.yml \
  --cherry-pick image \
  --cherry-pick resources \
  --cherry-pick autoscaling \
  | graft json > helm/production-values.json

# Install with Helm
helm upgrade --install my-app ./chart \
  -f helm/production-values.json \
  --namespace production
```

## ArgoCD Integration

### ApplicationSet with Graft

**Generate ArgoCD ApplicationSet:**

**config/argocd-template.yml:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: (( grab meta.app_name ))
spec:
  generators:
    - list:
        elements:
        (( for env in environments ))
          - environment: (( grab env.name ))
            namespace: (( grab env.namespace ))
            server: (( grab env.cluster ))
        (( done ))
  template:
    metadata:
      name: '{{environment}}-app'
    spec:
      project: default
      source:
        repoURL: (( grab meta.repo_url ))
        targetRevision: HEAD
        path: k8s/{{environment}}
      destination:
        server: '{{server}}'
        namespace: '{{namespace}}'
```

```sh
graft merge config/argocd-template.yml config/environments.yml > argocd/applicationset.yml
```

## See Also

- [Multi-Environment Setups](multi-environment.md) - Environment management patterns
- [Secrets Management](secrets-management.md) - CI/CD secrets integration
- [CLI Quick Reference](../reference/cli-quick-reference.md) - All CLI commands
