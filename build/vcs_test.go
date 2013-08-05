package main

import (
	"github.com/kisielk/gosrc"
	"testing"
	"time"
)

func revEqual(a, b gosrc.Revision) bool {
	return a.Id == b.Id && a.Author == b.Author && a.Date.Equal(b.Date)
}

func TestParseRevision(t *testing.T) {
	var testRev = `1234
2014-05-26 15:30:45 -0700
Kamil Kisiel <kamil@kamilkisiel.net>`

	rev := parseRevision(testRev)
	date, _ := time.Parse(iso8601Date, "2014-05-26 15:30:45 -0700")
	expectedRev := gosrc.Revision{
		Id:     "1234",
		Author: "Kamil Kisiel <kamil@kamilkisiel.net>",
		Date:   date,
	}
	if !revEqual(rev, expectedRev) {
		t.Fatalf("got %+v, want %+v", rev, expectedRev)
	}
}

func TestParseBzrRevision(t *testing.T) {
	var bzrRev = `------------------------------------------------------------
revno: 4
committer: Gustavo Niemeyer <gustavo@niemeyer.net>
branch nick: twik
timestamp: Tue 2013-07-16 19:19:43 -0300
message:
	Add a package doc.
`

	rev := parseBzrRevision(bzrRev)
	date, _ := time.Parse(bzrDate, "Tue 2013-07-16 19:19:43 -0300")
	expectedRev := gosrc.Revision{
		Id:     "4",
		Author: "Gustavo Niemeyer <gustavo@niemeyer.net>",
		Date:   date,
	}
	if !revEqual(rev, expectedRev) {
		t.Fatalf("got %+v, want %+v", rev, expectedRev)
	}
}
