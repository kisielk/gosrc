package main

import (
	"bytes"
	"os/exec"
	"strings"
)

func vcsCmd(dir, cmd string, args ...string) string {
	var buf bytes.Buffer
	c := exec.Command(cmd, args...)
	c.Dir = dir
	c.Stdout = &buf
	err := c.Run()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

type VCS interface {
	Name() string
	Revision(dir string) string
	Root(dir string) string
	URL(dir string) string
}

type git struct {
}

func (g git) Name() string {
	return "git"
}

func (g git) Revision(dir string) string {
	return vcsCmd(dir, "git", "rev-parse", "--verify", "HEAD")
}

func (g git) Root(dir string) string {
	return vcsCmd(dir, "git", "rev-parse", "--show-toplevel")
}

func (g git) URL(dir string) string {
	return vcsCmd(dir, "git", "config", "--get", "remote.origin.url")
}

type hg struct {
}

func (h hg) Name() string {
	return "hg"
}

func (h hg) Revision(dir string) string {
	return vcsCmd(dir, "hg", "id", "-i")
}

func (h hg) Root(dir string) string {
	return vcsCmd(dir, "hg", "root")
}

func (h hg) URL(dir string) string {
	return vcsCmd(dir, "hg", "paths", "default")
}

type bzr struct {
}

func (b bzr) Name() string {
	return "bzr"
}

func (b bzr) Revision(dir string) string {
	return vcsCmd(dir, "bzr", "revno")
}

func (b bzr) Root(dir string) string {
	return vcsCmd(dir, "bzr", "root")
}

func (b bzr) URL(dir string) string {
	return ""
}

var (
	Git    = git{}
	Hg     = hg{}
	Bzr    = bzr{}
	AllVCS = []VCS{Git, Hg, Bzr}
)
