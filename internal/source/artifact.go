package source

// ArtifactRef carries resolved artifact metadata from a Flux OCIRepository.
type ArtifactRef struct {
	// URL is the HTTP(S) address where the artifact can be fetched.
	URL string

	// Revision is the source revision string (e.g., "v0.0.1@sha256:abc...").
	Revision string

	// Digest is the artifact content digest (e.g., "sha256:abc...").
	Digest string
}
