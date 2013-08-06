package gosrc

import (
	"time"
)

type Repository struct {
	Type     string
	Revision Revision
	Root     string
	URL      string
}

type Revision struct {
	Id     string
	Date   time.Time
	Author string
}

type Package struct {
	ImportPath string
	Imports    []string
	Date       time.Time
	Repository Repository
	Build      Build
	Test       Test
	Vet        Vet
	Errcheck   Errcheck
	BuildInfo  BuildInfo
}

type Build struct {
	Succeeded bool
	Log       string
}

type Test struct {
	Succeeded bool
	Log       string
}

type Vet struct {
	Errors int
	Log    string
}

type Errcheck struct {
	Errors int
	Log    string
}

// BuildInfo contains info from go/build
type BuildInfo struct {
	Imports []string
	UsesCgo bool
}
