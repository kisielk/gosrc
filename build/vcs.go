package main

import (
	"bytes"
	"os/exec"
	"strings"
)

type VCS struct {
	cmd         string
	revisionCmd []string
	rootCmd     []string
}

func (v VCS) vcsCmd(dir string, args []string) string {
	var buf bytes.Buffer
	cmd := exec.Command(v.cmd, args...)
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

var git = VCS{
	cmd:         "git",
	revisionCmd: []string{"rev-parse", "--verify", "HEAD"},
	rootCmd:     []string{"rev-parse", "--show-toplevel"},
}

var hg = VCS{
	cmd:         "hg",
	revisionCmd: []string{"id", "-i"},
	rootCmd:     []string{"root"},
}

var bzr = VCS{
	cmd:         "bzr",
	revisionCmd: []string{"revno"},
	rootCmd:     []string{"root"},
}
