package config

import (
	"os"
	"strings"
	"testing"
)

func TestSetKeepsCommentsAndRoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := Load(); err != nil { // writes the commented default
		t.Fatal(err)
	}
	if err := Set("client_id", `abc-123.apps.googleusercontent.com`); err != nil {
		t.Fatal(err)
	}
	if err := Set("cookies_from_browser", "firefox"); err != nil {
		t.Fatal(err)
	}
	if err := Set("brand_new_key", `with "quotes" \ and slash`); err != nil {
		t.Fatal(err)
	}
	if err := Set("cookies_from_browser", ""); err != nil { // overwrite, not duplicate
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	s := string(b)
	if !strings.Contains(s, "# YouTube akkaunt bilan kirish") {
		t.Fatal("comments lost")
	}
	if strings.Count(s, "cookies_from_browser =") != 1 || strings.Count(s, "client_id") < 1 {
		t.Fatalf("duplicate/missing keys:\n%s", s)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("file no longer valid TOML: %v\n%s", err, s)
	}
	if c.ClientID != "abc-123.apps.googleusercontent.com" || c.CookiesFromBrowser != "" || c.Volume != 70 {
		t.Fatalf("round trip: %+v", c)
	}
	if st, _ := os.Stat(Path()); st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v (holds a client secret)", st.Mode().Perm())
	}
}
