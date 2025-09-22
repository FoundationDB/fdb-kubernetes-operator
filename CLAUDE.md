# FoundationDB Kubernetes Operator - Claude Development Guide

## Project Overview

The FoundationDB Kubernetes Operator is a sophisticated Kubernetes controller that manages FoundationDB clusters in Kubernetes environments. It automates deployment, scaling, backup, restore, and maintenance operations for FoundationDB distributed database clusters.

### Architecture Components

- **API Types** (`api/v1beta2/`): Custom Resource Definitions (CRDs) for FoundationDBCluster, FoundationDBBackup, FoundationDBRestore
- **Controllers** (`controllers/`): Reconciliation logic for cluster lifecycle management
- **Internal Packages** (`internal/`): Core business logic for coordination, replacements, maintenance, etc.
- **PKG Packages** (`pkg/`): Reusable components like admin clients, pod managers, status checks
- **E2E Tests** (`e2e/`): Comprehensive end-to-end testing with chaos engineering

### Key Patterns

- **Controller-Runtime**: Built on `sigs.k8s.io/controller-runtime` framework
- **Reconciliation Loops**: Event-driven state reconciliation
- **Mock-Based Testing**: Extensive mocking for unit tests
- **Chaos Engineering**: Production-like failure testing with chaos-mesh

## Development Environment Setup

### Prerequisites

```bash
# Go 1.24+ required
go version

# Install dependencies
make deps

# Install FoundationDB client package
# For macOS: Download from https://github.com/apple/foundationdb/releases
# For arm64 Mac: Make sure to install the arm64 package
```

### Local Development

```bash
# Clone repository
git clone https://github.com/FoundationDB/fdb-kubernetes-operator
cd fdb-kubernetes-operator

# Set up test certificates
config/test-certs/generate_secrets.bash

# Build and deploy operator (requires local K8s cluster)
make rebuild-operator

# Create test cluster
kubectl apply -k ./config/tests/base
```

## Build System & Tooling

### Primary Make Commands

| Command | Purpose |
|---------|---------|
| `make all` | Complete build pipeline: deps, generate, fmt, vet, test, build |
| `make test` | Run unit tests (Ginkgo with race detection if TEST_RACE_CONDITIONS=1) |
| `make lint` | Run golangci-lint with project rules |
| `make fmt` | Format code using golines + goimports + golangci-lint --fix |
| `make vet` | Run go vet static analysis |
| `make generate` | Generate deepcopy methods and CRDs |
| `make manifests` | Generate CRD YAML files |
| `make container-build` | Build Docker image |
| `make deploy` | Deploy operator to Kubernetes cluster |
| `make rebuild-operator` | Build, push (if remote), deploy, and bounce operator |

### Environment Variables

- `IMG`: Operator image name (default: `fdb-kubernetes-operator:latest`)
- `SIDECAR_IMG`: Sidecar image name
- `REMOTE_BUILD`: Set to 1 for remote builds (enables image push)
- `BUILD_PLATFORM`: Override build platform (e.g., `linux/amd64`)
- `TEST_RACE_CONDITIONS`: Set to 1 to enable race detection in tests
- `SKIP_TEST`: Set to 1 to skip tests in build

## Testing Framework

### Unit Testing (Ginkgo v2 + Gomega)

```go
// Example test structure from controllers/suite_test.go
var _ = Describe("ControllerName", func() {
    BeforeEach(func() {
        // Setup test environment
        k8sClient = mockclient.NewMockClient()
    })

    It("should reconcile successfully", func() {
        // Test implementation
        Expect(result).To(BeNil())
    })
})
```

### E2E Testing

- **Location**: `e2e/` directory with test packages
- **Framework**: Ginkgo + Gomega + chaos-mesh for failure injection
- **Types**: Upgrades, HA failures, stress testing, maintenance mode
- **Run**: `make test` with e2e labels

### Mock Objects

- **Kubernetes Client**: `mockclient.MockClient`
- **FDB Admin Client**: `mock.DatabaseClientProvider`
- **Pod Client**: `mockpodclient.NewMockFdbPodClient`

## Code Standards & Conventions

### Linting & Formatting

**golangci-lint Configuration** (`.golangci.yml`):
- **Enabled Linters**: errcheck, govet, staticcheck, revive, misspell, ineffassign, unused
- **Formatters**: gofmt with golines (120 char limit)
- **Dependency Guard**: Restricted import paths for clean architecture

**Formatting Tools**:
- `golines`: Line length formatting with gofmt base
- `goimports`: Import organization
- `golangci-lint run --fix`: Auto-fix issues

### Package Organization

```
├── api/v1beta2/           # CRD types and API definitions
├── controllers/           # Controller reconciliation logic
├── internal/              # Internal business logic packages
├── pkg/                   # Reusable library packages
├── e2e/                   # End-to-end tests
├── kubectl-fdb/           # kubectl plugin
├── fdbclient/             # FDB client utilities
└── setup/                 # Operator setup and configuration
```

### Naming Conventions

- **Types**: PascalCase (e.g., `FoundationDBCluster`)
- **Functions**: PascalCase for exported, camelCase for internal
- **Constants**: PascalCase or UPPER_SNAKE_CASE for public constants
- **Files**: snake_case.go
- **Test Files**: `*_test.go` with corresponding suite_test.go

### Error Handling

```go
// Preferred error handling pattern
err := someOperation()
if err != nil {
    return reconcile.Result{}, fmt.Errorf("failed to perform operation: %w", err)
}

// Use structured errors from internal/error_helper.go
return internal.ReconcileResult{
    Message: "Operation failed",
    Err:     err,
}
```

## Development Workflow

### 1. Understanding Existing Patterns

Before implementing new features:

```bash
# Find similar implementations
grep -r "similar_pattern" controllers/
grep -r "ProcessGroup" api/v1beta2/

# Study existing controller structure
ls controllers/*_controller.go
```

### 2. Controller Development

**Controller Structure**:
```go
type FoundationDBClusterReconciler struct {
    client.Client
    Log                     logr.Logger
    Recorder                record.EventRecorder
    DatabaseClientProvider  DatabaseClientProvider
    PodLifecycleManager     podmanager.PodLifecycleManager
}

func (r *FoundationDBClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
    // Implementation
}
```

### 3. API Changes

```bash
# After modifying API types in api/v1beta2/
make clean all  # Generate new CRD structs.
```

### 4. Testing Strategy

1. **Write unit tests first** using Ginkgo/Gomega
2. **Use mock clients** for Kubernetes operations
3. **Add e2e tests** for complex scenarios
4. **Run race detection**: `TEST_RACE_CONDITIONS=1 make test`

### 5. Common Development Tasks

**Adding a new CRD field**:
1. Modify struct in `api/v1beta2/*_types.go`
2. Add validation tags and documentation
3. Run `make generate manifests`
4. Add tests in `api/v1beta2/*_test.go`

**Adding a new controller reconciler**:
1. Create new file in `controllers/`
2. Implement reconciliation logic
3. Add to `main.go` and controller setup
4. Write comprehensive tests

## Key Libraries & Dependencies

### Core Dependencies

- **controller-runtime** (`sigs.k8s.io/controller-runtime`): Kubernetes operator framework
- **client-go** (`k8s.io/client-go`): Kubernetes API client
- **FoundationDB Bindings** (`github.com/apple/foundationdb/bindings/go`): FDB Go client
- **logr** (`github.com/go-logr/logr`): Structured logging interface

### Testing Dependencies

- **Ginkgo v2** (`github.com/onsi/ginkgo/v2`): BDD testing framework
- **Gomega** (`github.com/onsi/gomega`): Assertion library
- **chaos-mesh**: Chaos engineering for e2e tests

### Development Tools

- **controller-gen**: CRD and deepcopy generation
- **kustomize**: Kubernetes configuration management
- **golangci-lint**: Go linting
- **golines + goimports**: Code formatting
- **goreleaser**: Binary releases

## Debugging & Troubleshooting

### Local Debugging

```bash
# View operator logs
kubectl logs -f -l app=fdb-kubernetes-operator-controller-manager --container=manager

# Check cluster status
kubectl get foundationdbcluster test-cluster -o yaml

# Access FDB CLI
kubectl fdb exec -it test-cluster -- fdbcli
```

### Common Issues

1. **Build Failures**: Ensure FoundationDB client is installed for your platform
2. **Test Failures**: Check mock setup and race conditions
3. **CRD Issues**: Regenerate with `make manifests` after API changes
4. **Image Issues**: Verify BUILD_PLATFORM matches your cluster architecture

## Contributing Guidelines

### Before Opening PRs

1. **Run full test suite**: `make all`
2. **Check formatting**: `make fmt`
3. **Update documentation** if adding new features
4. **Add/update tests** for new functionality
5. **Follow existing patterns** in similar controllers

### Commit Messages

Follow conventional commit format:
```
feat(controller): add new reconciliation step for process replacement
fix(api): correct validation for database configuration
test(e2e): add chaos testing for network partitions
```

### Pull Request Process

1. Create feature branch from `main`
2. Implement changes following this guide
3. Ensure all tests pass
4. Update documentation if needed
5. Reference any related GitHub issues

## Additional Resources

- [FoundationDB Documentation](https://apple.github.io/foundationdb/)
- [controller-runtime Book](https://book.kubebuilder.io/)
- [Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [Community Forums](https://forums.foundationdb.org)
