package app

import "testing"

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("SQLITE_PATH", "users-test.db")
	if got := envOrDefault("SQLITE_PATH", "users.db"); got != "users-test.db" {
		t.Fatalf("envOrDefault=%q want=%q", got, "users-test.db")
	}
	if got := envOrDefault("UNSET_SQLITE_PATH", "users.db"); got != "users.db" {
		t.Fatalf("envOrDefault fallback=%q want=%q", got, "users.db")
	}
}

func TestEnvBoolOrDefault(t *testing.T) {
	t.Setenv("ALLOW_JSON_SAVE", "true")
	if got := envBoolOrDefault("ALLOW_JSON_SAVE", false); !got {
		t.Fatal("envBoolOrDefault should parse true")
	}
	t.Setenv("ALLOW_JSON_SAVE", "false")
	if got := envBoolOrDefault("ALLOW_JSON_SAVE", true); got {
		t.Fatal("envBoolOrDefault should parse false")
	}
	t.Setenv("ALLOW_JSON_SAVE", "broken")
	if got := envBoolOrDefault("ALLOW_JSON_SAVE", false); got {
		t.Fatal("envBoolOrDefault should fall back on invalid bool")
	}
	if got := envBoolOrDefault("UNSET_ALLOW_JSON_SAVE", true); !got {
		t.Fatal("envBoolOrDefault should fall back when unset")
	}
}
