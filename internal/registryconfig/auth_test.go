package registryconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncodedPullAuth_inlineGHCR(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	content := `{
		"auths": {
			"https://ghcr.io": {"auth": "dXNlcjp0b2tlbg=="}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	encoded, err := EncodedPullAuth("ghcr.io/org/app:latest")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "" {
		t.Fatal("expected encoded registry auth for ghcr.io")
	}
}

func TestEncodedPullAuth_missingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)

	encoded, err := EncodedPullAuth("ghcr.io/org/app:latest")
	if err != nil {
		t.Fatal(err)
	}
	if encoded != "" {
		t.Fatalf("expected empty auth without config, got %q", encoded)
	}
}

func TestSummarize(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("DOCKER_CONFIG", dir)
		sum, err := Summarize()
		if err != nil {
			t.Fatal(err)
		}
		if sum.HasAnyCredentials {
			t.Fatal("expected no credentials")
		}
		if len(sum.AuthHosts) != 0 {
			t.Fatalf("expected no auth hosts, got %v", sum.AuthHosts)
		}
	})

	t.Run("auth entries and helpers", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("DOCKER_CONFIG", dir)
		content := `{
			"auths": {
				"https://ghcr.io": {"auth": "Zm9vOmJhcg=="},
				"index.docker.io/v1/": {}
			},
			"credHelpers": {"ecr.example.com":"ecr-login"}
		}`
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		sum, err := Summarize()
		if err != nil {
			t.Fatal(err)
		}
		if !sum.HasAnyCredentials {
			t.Fatal("expected credentials to be detected")
		}
		if _, ok := sum.AuthHosts["ghcr.io"]; !ok {
			t.Fatalf("expected ghcr.io auth host, got %v", sum.AuthHosts)
		}
		if _, ok := sum.AuthHosts["docker.io"]; !ok {
			t.Fatalf("expected docker.io auth host normalization, got %v", sum.AuthHosts)
		}
	})
}
