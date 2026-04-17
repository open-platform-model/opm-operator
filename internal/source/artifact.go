package source

// ArtifactRef carries resolved artifact metadata from a Flux source object.
type ArtifactRef struct {
	// Kind is the source kind (OCIRepository, GitRepository, Bucket).
	// Determines extraction format: OCIRepository → zip, others → tar.gz.
	Kind string

	// URL is the HTTP(S) address where the artifact can be fetched.
	URL string

	// Revision is the source revision string (e.g., "v0.0.1@sha256:abc...").
	Revision string

	// Digest is the artifact content digest (e.g., "sha256:abc...").
	Digest string
}
