package main

import (
	"bytes"
	"github.com/kisielk/gosrc"
	"os/exec"
	"strings"
	"time"
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

const (
	iso8601Date = "2006-01-02 15:04:05 -0700"
	bzrDate     = "Mon 2006-01-02 15:04:05 -0700"
)

func parseRevision(s string) gosrc.Revision {
	var rev gosrc.Revision
	parts := strings.Split(s, "\n")
	if len(parts) == 3 {
		rev.Id = parts[0]
		d, _ := time.Parse(iso8601Date, parts[1])
		rev.Date = d
		rev.Author = parts[2]
	}
	return rev
}

func parseBzrRevision(s string) gosrc.Revision {
	var rev gosrc.Revision
	for _, l := range strings.Split(s, "\n") {
		parts := strings.SplitN(l, " ", 2)
		if len(parts) == 2 {
			switch parts[0] {
			case "revno:":
				rev.Id = parts[1]
			case "committer:":
				rev.Author = parts[1]
			case "timestamp:":
				d, _ := time.Parse(bzrDate, parts[1])
				rev.Date = d
			}
		}
	}
	return rev
}

type VCS interface {
	Name() string
	Revision(dir string) gosrc.Revision
	Root(dir string) string
	URL(dir string) string
}

type git struct {
}

func (g git) Name() string {
	return "git"
}

func (g git) Revision(dir string) gosrc.Revision {
	s := vcsCmd(dir, "git", "log", "--pretty=format:%h%n%ai%n%an <%ae>", "-1")
	return parseRevision(s)
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

func (h hg) Revision(dir string) gosrc.Revision {
	s := vcsCmd(dir, "hg", "log", "-r", ".", "--template", "{node|short}\n{date|isodatesec}\n{author}")
	return parseRevision(s)
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

func (b bzr) Revision(dir string) gosrc.Revision {
	//stupid bzr and its non-customizable output
	s := vcsCmd(dir, "bzr", "log", "--limit=1", "--log-format=long")
	return parseBzrRevision(s)
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
