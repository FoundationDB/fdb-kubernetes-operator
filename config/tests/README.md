This directory contains test cases for use in developing new features in the
operator. These are not guaranteed to be representative of how you would run
FoundationDB in a real environment. For basic examples of how to configure a
real FoundationDB installation, see the [samples](/config/samples) directory.
For more examples of how to customize a deployment, see the
[user manual](/docs/user_manual.md).

Most of these test cases are built using kustomize, and you can apply them by
using kubectl. Example: `kubectl apply -k config/tests/base`. The `multi_kc` and
`multi_dc` test cases have special test scripts to support a multi-stage
creation process. Example: `bash config/tests/multi_dc/create.bash`. Those
directories also contain `apply.bash` and `delete.bash` scripts.
