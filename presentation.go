package main

import (
	"regexp"
	"strings"
)

// Portage de public/js/presentation.js (docs/22) : {{var}} → la valeur ou
// rien ; {{#var}}…{{/var}} → le bloc n'est gardé que si la variable est
// non vide. Même contrat que le site, pour que le résultat soit identique.
var (
	reVar      = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)
	reTrailing = regexp.MustCompile(`(?m)[ \t]+$`)
	reBlank    = regexp.MustCompile(`\n{3,}`)
)

func renderTemplate(body string, data map[string]string) string {
	out := body
	for {
		start := strings.Index(out, "{{#")
		if start < 0 {
			break
		}
		nameEnd := strings.Index(out[start:], "}}")
		if nameEnd < 0 {
			break
		}
		name := out[start+3 : start+nameEnd]
		close := "{{/" + name + "}}"
		end := strings.Index(out[start:], close)
		if end < 0 {
			break
		}
		inner := out[start+nameEnd+2 : start+end]
		keep := ""
		if data[name] != "" {
			keep = inner
		}
		out = out[:start] + keep + out[start+end+len(close):]
	}
	out = reVar.ReplaceAllStringFunc(out, func(m string) string {
		key := reVar.FindStringSubmatch(m)[1]
		return data[key]
	})
	out = reTrailing.ReplaceAllString(out, "")
	out = reBlank.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

// wrapCaptures habille les URL d'images selon le format, une par ligne.
func wrapCaptures(format string, urls []string) string {
	lines := make([]string, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if format == "html" {
			lines = append(lines, `<img src="`+u+`" alt="">`)
		} else {
			lines = append(lines, "[img]"+u+"[/img]")
		}
	}
	return strings.Join(lines, "\n")
}
