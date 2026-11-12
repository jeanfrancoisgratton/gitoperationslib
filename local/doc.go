// Package local reads a git working copy straight from its .git directory:
// branches, upstreams, worktrees and remotes. It needs neither the git client
// nor a server, and never writes to the clone.
//
// Open finds the working copy a directory is part of:
//
//	repo, err := local.Open(".")
//	branches, err := repo.Branches()
//
// Linked worktrees and submodules, whose .git is a file, are followed. Git
// config includes ([include], [includeIf]) are not read.
package local
