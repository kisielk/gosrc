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
	Path       string
	Date       time.Time
	Repository Repository
	Build      bool
	Test       bool
}
