package config

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestBuildMigrationPlanRejectsUnrepresentableProfileNamesWithoutSideEffects(t *testing.T) {
	const unrepresentableUsername = "alice@neu"
	tests := []struct {
		name  string
		write func(*testing.T, Paths)
	}{
		{
			name: "legacy Meta YAML",
			write: func(t *testing.T, paths Paths) {
				encoded := base64.StdEncoding.EncodeToString([]byte(migrationSecretCanary))
				writeMigrationFixture(t, paths.LegacyMetaYAML, fmt.Sprintf(`default_account: valid-first
accounts:
  - username: valid-first
    password: %s
  - username: %s
    password: %s
`, encoded, unrepresentableUsername, encoded))
			},
		},
		{
			name: "legacy neucn JSON",
			write: func(t *testing.T, paths Paths) {
				writeMigrationFixture(t, paths.LegacyUpstream, fmt.Sprintf(`{
  "default_account": "valid-first",
  "accounts": [
    {"username": "valid-first", "encrypted_password": "synthetic-ciphertext"},
    {"username": %q, "encrypted_password": "synthetic-ciphertext"}
  ]
}`, unrepresentableUsername))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := migrationFixturePaths(t)
			test.write(t, paths)

			plan, err := BuildMigrationPlan(paths, Default())
			if err == nil {
				plan.Close()
				t.Fatal("BuildMigrationPlan() silently accepted an unrepresentable profile name")
			}
			if strings.Contains(err.Error(), unrepresentableUsername) {
				t.Fatalf("migration error exposed the legacy username: %v", err)
			}
			assertMigrationPlanningSideEffectFree(t, paths)
			assertMigrationApplySideEffectFree(t, paths)
		})
	}
}
