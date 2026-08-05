package artifact

// Checksum returns a stable SHA-256 digest for the complete artifact manifest.
// The manifest includes the migration ID, risk metadata, warnings, and hashes of
// every payload file.
func (a Artifact) Checksum() (string, error) {
	manifest, err := a.MarshalManifest()
	if err != nil {
		return "", err
	}
	return digest(manifest), nil
}
