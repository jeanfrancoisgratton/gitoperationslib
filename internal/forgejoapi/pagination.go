package forgejoapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// ListQuery converts ListOptions into Forgejo's page/limit query parameters.
// Zero values are omitted so the server defaults apply; the server also caps
// limit at its configured maximum.
func ListQuery(opts gitoperationslib.ListOptions) url.Values {
	q := url.Values{}
	if opts.Page > 0 {
		q.Set("page", strconv.Itoa(opts.Page))
	}
	if opts.PerPage > 0 {
		q.Set("limit", strconv.Itoa(opts.PerPage))
	}
	return q
}

// nextPage returns the page number of the rel="next" link in an RFC 8288
// Link header, or 0 if there is none.
func nextPage(h http.Header) int {
	for _, header := range h.Values("Link") {
		rest := header
		for {
			start := strings.IndexByte(rest, '<')
			if start < 0 {
				break
			}
			end := strings.IndexByte(rest[start:], '>')
			if end < 0 {
				break
			}
			target := rest[start+1 : start+end]
			rest = rest[start+end+1:]

			params := rest
			if i := strings.IndexByte(params, '<'); i >= 0 {
				params = params[:i]
			}
			if !hasRel(params, "next") {
				continue
			}
			u, err := url.Parse(target)
			if err != nil {
				return 0
			}
			page, err := strconv.Atoi(u.Query().Get("page"))
			if err != nil {
				return 0
			}
			return page
		}
	}
	return 0
}

// hasRel reports whether a link's parameter list (`; rel="next"`) contains rel.
func hasRel(params, rel string) bool {
	for _, p := range strings.Split(params, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "rel") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(value), ",")), `"`)
		for _, r := range strings.Fields(value) {
			if strings.EqualFold(r, rel) {
				return true
			}
		}
	}
	return false
}

func totalCount(h http.Header) int {
	n, err := strconv.Atoi(h.Get("X-Total-Count"))
	if err != nil {
		return -1
	}
	return n
}
