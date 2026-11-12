package bitbucket

import (
	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/unsupported"
)

// Areas Bitbucket Cloud does not have. Every method returns ErrNotSupported,
// with the reason.

const (
	noReleases      = "Bitbucket Cloud has no releases (only a repository-wide Downloads area)"
	noTagProtection = "Bitbucket Cloud has no tag protection"
	noIssues        = "the Bitbucket Cloud issue tracker has been retired (CHANGE-3071)"
	noDirectory     = "the user and workspace directory is not implemented for Bitbucket Cloud"
	noMirrors       = "Bitbucket Cloud's API manages no mirrors"
	noGitHooks      = "Bitbucket Cloud runs no server-side git hooks"
	noSite          = "bitbucket.org is not administered through the API"
)

// Releases implements gitoperationslib.Client.
func (c *Client) Releases() gitoperationslib.Releases {
	return unsupported.Releases{Backend: gitoperationslib.BackendBitbucket, Reason: noReleases}
}

// TagProtections implements gitoperationslib.Client.
func (c *Client) TagProtections() gitoperationslib.TagProtections {
	return unsupported.TagProtections{Backend: gitoperationslib.BackendBitbucket, Reason: noTagProtection}
}

// Issues implements gitoperationslib.Client.
func (c *Client) Issues() gitoperationslib.Issues {
	return unsupported.Issues{Backend: gitoperationslib.BackendBitbucket, Reason: noIssues}
}

// Labels implements gitoperationslib.Client.
func (c *Client) Labels() gitoperationslib.Labels {
	return unsupported.Labels{Backend: gitoperationslib.BackendBitbucket, Reason: noIssues}
}

// Milestones implements gitoperationslib.Client.
func (c *Client) Milestones() gitoperationslib.Milestones {
	return unsupported.Milestones{Backend: gitoperationslib.BackendBitbucket, Reason: noIssues}
}

// Mirrors implements gitoperationslib.Client.
func (c *Client) Mirrors() gitoperationslib.Mirrors {
	return unsupported.Mirrors{Backend: gitoperationslib.BackendBitbucket, Reason: noMirrors}
}

// GitHooks implements gitoperationslib.Client.
func (c *Client) GitHooks() gitoperationslib.GitHooks {
	return unsupported.GitHooks{Backend: gitoperationslib.BackendBitbucket, Reason: noGitHooks}
}

// Site implements gitoperationslib.Client.
func (c *Client) Site() gitoperationslib.Site {
	return unsupported.Site{Backend: gitoperationslib.BackendBitbucket, Reason: noSite}
}

// Users implements gitoperationslib.Client: Me alone is implemented.
func (c *Client) Users() gitoperationslib.Users {
	return users{c, unsupported.Users{Backend: gitoperationslib.BackendBitbucket, Reason: noDirectory}}
}

type users struct {
	c *Client
	unsupported.Users
}

// Orgs implements gitoperationslib.Client.
func (c *Client) Orgs() gitoperationslib.Orgs {
	return unsupported.Orgs{Backend: gitoperationslib.BackendBitbucket, Reason: noDirectory}
}

// Teams implements gitoperationslib.Client.
func (c *Client) Teams() gitoperationslib.Teams {
	return unsupported.Teams{Backend: gitoperationslib.BackendBitbucket, Reason: noDirectory}
}
