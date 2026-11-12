package local

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Remote is a remote of the working copy.
type Remote struct {
	Name string `json:"name"`
	URL  string `json:"url"` // the first url line; git fetches from it
}

// Remotes lists the remotes, sorted by name.
func (r *Repo) Remotes() ([]Remote, error) {
	cfg, err := r.config()
	if err != nil {
		return nil, err
	}
	var out []Remote
	for key, vals := range cfg {
		if name, ok := strings.CutPrefix(key, "remote."); ok {
			if name, ok = strings.CutSuffix(name, ".url"); ok && len(vals) > 0 {
				out = append(out, Remote{Name: name, URL: vals[0]})
			}
		}
	}
	slices.SortFunc(out, func(a, b Remote) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// ParseRemoteURL splits a remote's URL: https://forge/owner/repo(.git),
// ssh://git@forge:2222/owner/repo, git@forge:owner/repo.git... ok is false for
// what points to no server (a local path, file://).
func ParseRemoteURL(remote string) (host, owner, repo string, ok bool) {
	var path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" {
			return "", "", "", false
		}
		host, path = u.Hostname(), u.Path
	} else {
		// scp-like syntax: git itself takes a colon after the first slash as part of a local path
		h, p, found := strings.Cut(remote, ":")
		if !found || strings.Contains(h, "/") {
			return "", "", "", false
		}
		if i := strings.LastIndex(h, "@"); i >= 0 {
			h = h[i+1:]
		}
		host, path = h, p
	}
	parts := strings.Split(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	n := len(parts)
	if n < 2 || parts[n-2] == "" || parts[n-1] == "" {
		return "", "", "", false
	}
	return host, parts[n-2], parts[n-1], true
}

// SameHost compares a forge's hosts: the SSH and API hosts are often separate
// from the web one (ssh.bitbucket.org, api.github.com).
func SameHost(a, b string) bool {
	norm := func(h string) string {
		h = strings.ToLower(h)
		for _, prefix := range []string{"altssh.", "ssh.", "api.", "www."} {
			if trimmed, ok := strings.CutPrefix(h, prefix); ok {
				return trimmed
			}
		}
		return h
	}
	return norm(a) == norm(b)
}

// RemoteRepo is the repository on a server that a remote points to.
type RemoteRepo struct {
	Owner  string `json:"owner"`
	Name   string `json:"name"`
	Remote string `json:"remote"` // the remote it was read from
}

// Errors of RepoOn, matched with errors.Is.
var (
	ErrNoRemote        = errors.New("the working copy has no remote")
	ErrNoRemoteOnHost  = errors.New("no remote of the working copy is on this server")
	ErrAmbiguousRemote = errors.New("the working copy's remotes on this server point to different repositories")
)

// RepoOn finds the repository this working copy is a clone of on the server at
// serverURL. Only the remotes on that server count: another forge's repository
// of the same name is not the one asked about. Among several, origin wins;
// failing that, they must all point to the same repository.
func (r *Repo) RepoOn(serverURL string) (*RemoteRepo, error) {
	remotes, err := r.Remotes()
	if err != nil {
		return nil, err
	}
	if len(remotes) == 0 {
		return nil, ErrNoRemote
	}
	if !strings.Contains(serverURL, "://") {
		serverURL = "https://" + serverURL
	}
	server, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}
	var found []RemoteRepo
	var seen []string
	for _, rm := range remotes {
		host, owner, name, ok := ParseRemoteURL(rm.URL)
		if !ok {
			continue
		}
		seen = append(seen, rm.Name+" is on "+host)
		if SameHost(host, server.Hostname()) {
			if rm.Name == "origin" {
				return &RemoteRepo{Owner: owner, Name: name, Remote: rm.Name}, nil
			}
			found = append(found, RemoteRepo{Owner: owner, Name: name, Remote: rm.Name})
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("%w: %s (%s)", ErrNoRemoteOnHost, server.Hostname(), strings.Join(seen, ", "))
	}
	for _, f := range found[1:] {
		if !strings.EqualFold(f.Owner, found[0].Owner) || !strings.EqualFold(f.Name, found[0].Name) {
			names := make([]string, len(found))
			for i, f := range found {
				names[i] = f.Remote + ": " + f.Owner + "/" + f.Name
			}
			return nil, fmt.Errorf("%w: %s (%s), and none is called origin", ErrAmbiguousRemote,
				server.Hostname(), strings.Join(names, ", "))
		}
	}
	return &found[0], nil
}

// MatchesRepo tells if one of the remotes points to owner/repo, whatever the
// host and protocol. An empty owner matches any owner.
func (r *Repo) MatchesRepo(owner, repo string) (bool, error) {
	remotes, err := r.Remotes()
	if err != nil {
		return false, err
	}
	for _, rm := range remotes {
		if _, o, n, ok := ParseRemoteURL(rm.URL); ok &&
			(owner == "" || strings.EqualFold(o, owner)) && strings.EqualFold(n, repo) {
			return true, nil
		}
	}
	return false, nil
}
