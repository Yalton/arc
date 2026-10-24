package main

import (
	"errors"
	"os"

	"arc/internal/gitx"
	"arc/internal/registry"
)

// repo is the repository a command operates on.
type repo struct {
	root  string
	reg   *registry.Registry
	files []string
}

// repoRoot returns the git top-level of the working directory, or the
// working directory itself outside git.
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if root, err := gitx.Root(wd); err == nil {
		return root, nil
	}
	return wd, nil
}

// openRepo finds the repository root and loads its registry and file list.
func openRepo() (*repo, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	reg, err := registry.Load(root)
	if err != nil {
		return nil, err
	}
	files, err := registry.RepoFiles(root)
	if err != nil {
		return nil, errors.New("listing files: " + err.Error())
	}
	return &repo{root: root, reg: reg, files: files}, nil
}
