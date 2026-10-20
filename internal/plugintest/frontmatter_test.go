package plugintest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const root = "../.."

// frontmatter parses the leading YAML block of path strictly: unknown
// structure is fine, but the YAML itself must be valid.
func frontmatter(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		t.Fatalf("%s: does not start with ---", path)
	}
	var fm []byte
	if i := bytes.Index(rest, []byte("\n---\n")); i >= 0 {
		fm = rest[:i+1]
	} else if b, ok := bytes.CutSuffix(rest, []byte("\n---")); ok {
		fm = append(b, '\n')
	} else {
		t.Fatalf("%s: frontmatter is not closed", path)
	}
	dec := yaml.NewDecoder(bytes.NewReader(fm))
	dec.KnownFields(true)
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func TestSkillsFrontmatter(t *testing.T) {
	skills, _ := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
	agents, _ := filepath.Glob(filepath.Join(root, "agents", "*.md"))
	if len(skills) == 0 || len(agents) == 0 {
		t.Fatalf("found %d skills, %d agents", len(skills), len(agents))
	}
	for _, p := range append(skills, agents...) {
		isSkill := filepath.Base(p) == "SKILL.md"
		want := strings.TrimSuffix(filepath.Base(p), ".md")
		if isSkill {
			want = filepath.Base(filepath.Dir(p))
		}
		t.Run(want, func(t *testing.T) {
			m := frontmatter(t, p)
			if name, _ := m["name"].(string); name != want {
				t.Errorf("name = %q, want %q", m["name"], want)
			}
			if d, _ := m["description"].(string); strings.TrimSpace(d) == "" {
				t.Errorf("description is empty")
			}
			if isSkill && (want == "create" || want == "check") && m["disable-model-invocation"] != true {
				t.Errorf("disable-model-invocation = %v, want true", m["disable-model-invocation"])
			}
			if !isSkill {
				var tools []string
				switch v := m["tools"].(type) {
				case string:
					tools = strings.Split(v, ",")
				case []any:
					for _, e := range v {
						s, _ := e.(string)
						tools = append(tools, s)
					}
				}
				for _, tl := range tools {
					switch strings.TrimSpace(tl) {
					case "Write", "Edit", "Bash":
						t.Errorf("tools contains %q", strings.TrimSpace(tl))
					}
				}
			}
		})
	}
}
