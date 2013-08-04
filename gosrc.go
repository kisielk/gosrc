package gosrc

import (
	"time"
)

type Repository struct {
	Type     string
	Revision string
	Root     string
}

type Package struct {
	ImportPath string
	Date       time.Time
	Repository Repository
	Build      Build
	Test       Test
}

type Build struct {
	Succeeded bool
	Log       string
}

type Test struct {
	Succeeded bool
	Log       string
}
