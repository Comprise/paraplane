package plugintest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const root = "../.."

// skillMeta and agentMeta list every frontmatter key the Claude Code docs
// (code.claude.com/docs/en/slash-commands and /sub-agents) define for skills
// and subagents. Claude Code silently ignores any other key, so a typo would
// go unnoticed; the strict decode fails on it instead.
type skillMeta struct {
	Name                   string    `yaml:"name"`
	Description            string    `yaml:"description"`
	WhenToUse              string    `yaml:"when_to_use"`
	ArgumentHint           string    `yaml:"argument-hint"`
	Arguments              any       `yaml:"arguments"`
	DisableModelInvocation *bool     `yaml:"disable-model-invocation"`
	UserInvocable          *bool     `yaml:"user-invocable"`
	AllowedTools           any       `yaml:"allowed-tools"`
	DisallowedTools        any       `yaml:"disallowed-tools"`
	Model                  string    `yaml:"model"`
	Effort                 string    `yaml:"effort"`
	Context                string    `yaml:"context"`
	Agent                  string    `yaml:"agent"`
	Background             *bool     `yaml:"background"`
	Hooks                  yaml.Node `yaml:"hooks"`
	Paths                  any       `yaml:"paths"`
	Shell                  string    `yaml:"shell"`
	Metadata               yaml.Node `yaml:"metadata"`
	License                string    `yaml:"license"`
	Compatibility          string    `yaml:"compatibility"`
}

type agentMeta struct {
	Name            string    `yaml:"name"`
	Description     string    `yaml:"description"`
	Tools           any       `yaml:"tools"`
	DisallowedTools any       `yaml:"disallowedTools"`
	Model           string    `yaml:"model"`
	PermissionMode  string    `yaml:"permissionMode"`
	MaxTurns        *int      `yaml:"maxTurns"`
	Skills          any       `yaml:"skills"`
	MCPServers      yaml.Node `yaml:"mcpServers"`
	Hooks           yaml.Node `yaml:"hooks"`
	Memory          string    `yaml:"memory"`
	Background      *bool     `yaml:"background"`
	OmitClaudeMd    *bool     `yaml:"omitClaudeMd"`
	Effort          string    `yaml:"effort"`
	Isolation       string    `yaml:"isolation"`
	Color           string    `yaml:"color"`
	InitialPrompt   string    `yaml:"initialPrompt"`
	Experimental    yaml.Node `yaml:"experimental"`
}

// frontmatter decodes the leading YAML block of path into out strictly: the
// YAML must be valid and use only the keys out declares.
func frontmatter(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		return fmt.Errorf("%s: does not start with ---", path)
	}
	var fm []byte
	if i := bytes.Index(rest, []byte("\n---\n")); i >= 0 {
		fm = rest[:i+1]
	} else if b, ok := bytes.CutSuffix(rest, []byte("\n---")); ok {
		fm = append(b, '\n')
	} else {
		return fmt.Errorf("%s: frontmatter is not closed", path)
	}
	dec := yaml.NewDecoder(bytes.NewReader(fm))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	return nil
}

// toolList splits a tools value (comma-separated string or YAML list) into
// trimmed, non-empty entries.
func toolList(v any) ([]string, error) {
	var raw []string
	switch v := v.(type) {
	case string:
		raw = strings.Split(v, ",")
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("tools entry %v is not a string", e)
			}
			raw = append(raw, s)
		}
	default:
		return nil, fmt.Errorf("tools has type %T", v)
	}
	var tools []string
	for _, s := range raw {
		if s = strings.TrimSpace(s); s != "" {
			tools = append(tools, s)
		}
	}
	return tools, nil
}

func TestFrontmatterRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "s.md", "---\nname: s\ndescription: d\nbogus: 1\n---\nbody\n")
	write(t, dir, "a.md", "---\nname: a\ndescription: d\ndisallowed-tools: Bash\n---\nbody\n")
	var sm skillMeta
	if err := frontmatter(filepath.Join(dir, "s.md"), &sm); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("skill with unknown key: err = %v", err)
	}
	var am agentMeta // the skill spelling is not an agent key
	if err := frontmatter(filepath.Join(dir, "a.md"), &am); err == nil || !strings.Contains(err.Error(), "disallowed-tools") {
		t.Errorf("agent with skill-only key: err = %v", err)
	}
}

func TestToolListDropsEmpty(t *testing.T) {
	for _, in := range []any{"", " , ", []any{}, []any{" ", ""}} {
		if got, err := toolList(in); err != nil || len(got) != 0 {
			t.Errorf("toolList(%q) = %q, %v; want empty", in, got, err)
		}
	}
	if got, _ := toolList(" Read, ,Grep "); strings.Join(got, "|") != "Read|Grep" {
		t.Errorf("toolList = %q", got)
	}
	if _, err := toolList(nil); err == nil {
		t.Error("toolList(nil) gave no error")
	}
}

func TestSkillsFrontmatter(t *testing.T) {
	skills, _ := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
	if len(skills) == 0 {
		t.Fatal("found no skills")
	}
	for _, p := range skills {
		want := filepath.Base(filepath.Dir(p))
		t.Run(want, func(t *testing.T) {
			var m skillMeta
			if err := frontmatter(p, &m); err != nil {
				t.Fatal(err)
			}
			if m.Name != want {
				t.Errorf("name = %q, want %q", m.Name, want)
			}
			if strings.TrimSpace(m.Description) == "" {
				t.Errorf("description is empty")
			}
			if (want == "create" || want == "check") && (m.DisableModelInvocation == nil || !*m.DisableModelInvocation) {
				t.Errorf("disable-model-invocation is not true")
			}
		})
	}
}

func TestAgentsFrontmatter(t *testing.T) {
	agents, _ := filepath.Glob(filepath.Join(root, "agents", "*.md"))
	if len(agents) == 0 {
		t.Fatal("found no agents")
	}
	for _, p := range agents {
		want := strings.TrimSuffix(filepath.Base(p), ".md")
		t.Run(want, func(t *testing.T) {
			var m agentMeta
			if err := frontmatter(p, &m); err != nil {
				t.Fatal(err)
			}
			if m.Name != want {
				t.Errorf("name = %q, want %q", m.Name, want)
			}
			if strings.TrimSpace(m.Description) == "" {
				t.Errorf("description is empty")
			}
			if m.Tools == nil {
				t.Fatal("agent has no tools: key (it would inherit every tool)")
			}
			tools, err := toolList(m.Tools)
			if err != nil {
				t.Fatal(err)
			}
			if len(tools) == 0 {
				t.Error("tools lists no tool")
			}
			for _, tl := range tools {
				switch tl {
				case "Write", "Edit", "Bash", "NotebookEdit":
					t.Errorf("tools contains %q", tl)
				}
			}
		})
	}
}
