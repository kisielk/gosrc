package gosrc

import (
	"encoding/json"
	"net/http"
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
	Gofmt      Gofmt
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

type Gofmt struct {
	Differences int
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
	GoFiles []string
}

// GodocPackages retrieves a list of packages in the godoc.org index
func GodocPackages() ([]string, error) {
	var results []string

	resp, err := http.Get("http://api.godoc.org/packages")
	if err != nil {
		return results, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var w struct {
		Results []struct {
			Path string `json:"path"`
		} `json:"results"`
	}
	err = dec.Decode(&w)
	if err != nil {
		return results, err
	}

	for _, result := range w.Results {
		results = append(results, result.Path)
	}

	return results, nil
}
