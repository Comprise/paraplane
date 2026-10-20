// Package plan extracts the ```paraplane block from a Markdown plan and
// parses it strictly into a Plan.
package plan

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// Plan is the parsed ```paraplane block.
type Plan struct {
	Version int    `yaml:"version"`
	Gate    string `yaml:"gate"`
	Waves   []Wave `yaml:"waves"`
}

// Wave is a set of tasks that may run in parallel.
type Wave struct {
	ID    string `yaml:"id"`
	Gate  string `yaml:"gate"`
	Tasks []Task `yaml:"tasks"`
}

// Task is one unit of work in a wave.
type Task struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Owns    []string `yaml:"owns"`
	Reads   []string `yaml:"reads"`
	Depends []string `yaml:"depends"`
	Verify  string   `yaml:"verify"`
}

// Limits that protect the hooks from a hostile plan.
const (
	MaxBlock      = 256 << 10 // bytes in the block body
	MaxTasks      = 500       // tasks in the plan
	MaxPaths      = 100       // owns + reads of one task
	MaxTotalPaths = 2000      // owns + reads of the whole plan
)

// SyntaxError reports YAML that does not parse.
type SyntaxError struct{ Msg string }

func (e *SyntaxError) Error() string { return e.Msg }

// LimitError reports a block that exceeds one of the limits.
type LimitError struct{ Msg string }

func (e *LimitError) Error() string { return e.Msg }

// SchemaError reports an unknown key, a wrong type or an unsupported version.
type SchemaError struct{ Msg string }

func (e *SchemaError) Error() string { return e.Msg }

const quoteHint = "; quote glob patterns"

// oneLine joins a multi-line message into a single line.
func oneLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	parts := lines[:0]
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, "; ")
}

// Parse decodes the body of a ```paraplane block. Unknown keys are errors.
func Parse(body []byte) (*Plan, error) {
	if len(body) > MaxBlock {
		return nil, &LimitError{Msg: fmt.Sprintf("paraplane block is %d bytes, the limit is %d", len(body), MaxBlock)}
	}
	var p Plan
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil && !errors.Is(err, io.EOF) {
		return nil, decodeError(err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, &SyntaxError{Msg: "more than one YAML document in the paraplane block"}
	} else if !errors.Is(err, io.EOF) {
		return nil, decodeError(err)
	}
	if p.Version != 0 && p.Version != 1 {
		return nil, &SchemaError{Msg: fmt.Sprintf("unsupported version %d (only 1 is supported)", p.Version)}
	}
	if err := checkLimits(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func decodeError(err error) error {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		return &SchemaError{Msg: oneLine("yaml: " + strings.Join(te.Errors, "\n"))}
	}
	msg := oneLine(err.Error())
	if strings.Contains(msg, "did not find expected alphabetic or numeric character") ||
		strings.Contains(msg, "did not find expected ',' or ']'") {
		msg += quoteHint
	}
	return &SyntaxError{Msg: msg}
}

func checkLimits(p *Plan) error {
	tasks, total := 0, 0
	for _, w := range p.Waves {
		for _, t := range w.Tasks {
			tasks++
			if tasks > MaxTasks {
				return &LimitError{Msg: fmt.Sprintf("more than %d tasks in the plan", MaxTasks)}
			}
			n := len(t.Owns) + len(t.Reads)
			if n > MaxPaths {
				return &LimitError{Msg: fmt.Sprintf("task %s has %d paths in owns and reads, the limit is %d", t.ID, n, MaxPaths)}
			}
			total += n
			if total > MaxTotalPaths {
				return &LimitError{Msg: fmt.Sprintf("more than %d paths in owns and reads of the plan", MaxTotalPaths)}
			}
		}
	}
	return nil
}

// Load extracts the ```paraplane block from md and parses it.
func Load(md []byte) (*Plan, error) {
	b, err := Extract(md)
	if err != nil {
		return nil, err
	}
	return Parse(b.Body)
}

// Tasks returns all tasks of the plan in the author's order. The pointers
// refer to the tasks inside p.
func (p *Plan) Tasks() []*Task {
	var out []*Task
	for i := range p.Waves {
		for j := range p.Waves[i].Tasks {
			out = append(out, &p.Waves[i].Tasks[j])
		}
	}
	return out
}
