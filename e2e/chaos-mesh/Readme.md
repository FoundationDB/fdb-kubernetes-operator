# Fork of the chaos-mesh struct

Original code from: https://github.com/chaos-mesh/chaos-mesh/tree/v2.6.7

We copied the structs inside this repository to decouple the dependencies as only the structs are required for our e2e test cases.
Before this separation it was hard/impossible to upgrade the `controller-runtime` dependency independent of the chaos-mesh release.

Steps to update the dependencies:

```bash
cd chaos-mesh
# Replace the tag with the desired version
git clone --depth 1 --branch v2.6.7 git@github.com:chaos-mesh/chaos-mesh.git
# Remove the current API version and copy the new version.
rm -rf ./api
mv ./chaos-mesh/api .
rm -rf chaos-mesh
# Delete all webhook files, we don't use them
rm -rf ./api/genericwebhook
find ./api/v1alpha1 -name "*webhook*" -delete
# Delete all test files
find ./api/v1alpha1 -name "*test.go" -delete
# Delete the go mod files
rm ./chaos-mesh/api/go.mod ./chaos-mesh/api/go.sum
```
