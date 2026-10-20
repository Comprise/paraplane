package plugintest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func readJSON(t *testing.T, rel string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return m
}

func TestCodexManifest(t *testing.T) {
	codex := readJSON(t, ".codex-plugin/plugin.json")
	claude := readJSON(t, ".claude-plugin/plugin.json")
	for _, k := range []string{"name", "description", "author", "license", "homepage", "repository", "keywords"} {
		if !reflect.DeepEqual(codex[k], claude[k]) {
			t.Errorf("%s: codex %v != claude %v", k, codex[k], claude[k])
		}
	}
	if codex["name"] != "paraplane" {
		t.Errorf("name = %v", codex["name"])
	}
	if codex["hooks"] != "./hooks/codex.json" {
		t.Errorf("hooks = %v", codex["hooks"])
	}
	if _, ok := codex["version"]; ok {
		t.Error("version must not be set on main")
	}
	readJSON(t, "hooks/codex.json") // must exist and be valid JSON
}

func TestCodexMarketplace(t *testing.T) {
	m := readJSON(t, ".agents/plugins/marketplace.json")
	if m["name"] != "paraplane" {
		t.Errorf("marketplace name = %v", m["name"])
	}
	plugins, _ := m["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(plugins))
	}
	p, _ := plugins[0].(map[string]any)
	if p["name"] != "paraplane" {
		t.Errorf("name = %v", p["name"])
	}
	src, _ := p["source"].(map[string]any)
	if src["source"] != "local" || src["path"] != "./" {
		t.Errorf("source = %v, want local ./", src)
	}
}
