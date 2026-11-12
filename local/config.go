package local

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// config is a git config file: values by "section.subsection.key", sections
// and keys lowercased, subsections kept as written. A key given several times
// keeps every value, in order.
type config map[string][]string

func (c config) get(key string) string {
	if v := c[key]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

// readConfig reads a git config file; a missing one is empty.
func readConfig(path string) (config, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return config{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := config{}
	section := ""
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "[") {
			end := strings.LastIndex(line, "]")
			if end < 0 {
				continue
			}
			section = sectionName(line[1:end])
			line = strings.TrimSpace(line[end+1:]) // "[core] bare = true" is valid
			if line == "" {
				continue
			}
		}
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		key, val, found := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !found {
			val = "true" // a bare key is a boolean
		}
		c[section+"."+key] = append(c[section+"."+key], configValue(val))
	}
	return c, s.Err()
}

// sectionName turns `remote "origin"` into "remote.origin". The old form
// [remote.origin] is lowercased whole, as git does.
func sectionName(h string) string {
	h = strings.TrimSpace(h)
	if name, sub, ok := strings.Cut(h, " "); ok {
		sub = strings.TrimSpace(sub)
		sub = strings.TrimSuffix(strings.TrimPrefix(sub, `"`), `"`)
		sub = strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(sub)
		return strings.ToLower(name) + "." + sub
	}
	return strings.ToLower(h)
}

// configValue unquotes a value and drops a trailing comment.
func configValue(v string) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(v); i++ {
		switch ch := v[i]; {
		case ch == '"':
			quoted = !quoted
		case ch == '\\' && i+1 < len(v):
			i++
			switch v[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(v[i])
			}
		case (ch == '#' || ch == ';') && !quoted:
			return strings.TrimSpace(b.String())
		default:
			b.WriteByte(ch)
		}
	}
	return strings.TrimSpace(b.String())
}
