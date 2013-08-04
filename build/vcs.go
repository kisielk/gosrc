package main

import (
	"bytes"
	"os/exec"
	"strings"
)

type VCS struct {
	Cmd         string
	revisionCmd []string
	rootCmd     []string
	urlCmd      []string
}

func (v VCS) vcsCmd(dir string, args []string) string {
	var buf bytes.Buffer
	cmd := exec.Command(v.Cmd, args...)
	cmd.Dir = dir
	cmd.Stdout = &buf
	err := cmd.Run()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

func (v VCS) Revision(dir string) string {
	return v.vcsCmd(dir, v.revisionCmd)
}

func (v VCS) Root(dir string) string {
	return v.vcsCmd(dir, v.rootCmd)
}

func (v VCS) URL(dir string) string {
	return v.vcsCmd(dir, v.urlCmd)
}

var git = VCS{
	Cmd:         "git",
	revisionCmd: []string{"rev-parse", "--verify", "HEAD"},
	rootCmd:     []string{"rev-parse", "--show-toplevel"},
	urlCmd:      []string{"config", "--get", "remote.origin.url"},
}

var hg = VCS{
	Cmd:         "hg",
	revisionCmd: []string{"id", "-i"},
	rootCmd:     []string{"root"},
	urlCmd:      []string{"paths", "default"},
}

var bzr = VCS{
	Cmd:         "bzr",
	revisionCmd: []string{"revno"},
	rootCmd:     []string{"root"},
	urlCmd:      []string{""}, // FIXME: What do we do here?
}
