# Scripts

- `observe-ocirepository.sh` prints the full `OCIRepository` and the key artifact fields needed for the experiment.
- `fetch-and-validate.sh` downloads the Flux artifact, attempts recovery of a module tree, and runs a trivial CUE validation.

These scripts are intentionally small and disposable. They are here to prove the source contract, not to become production controller code.
