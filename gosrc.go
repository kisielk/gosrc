package gosrc

import (
	"encoding/json"
	"fmt"
	"go/build"
	"labix.org/v2/mgo"
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
	Downloaded bool
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

// NewBuildInfo creates a BuildInfo from a build.Package
func NewBuildInfo(pkg *build.Package) BuildInfo {
	return BuildInfo{
		Imports: pkg.Imports,
		UsesCgo: len(pkg.CgoFiles) > 0,
		GoFiles: pkg.GoFiles,
	}
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

type Collection interface {
	Insert(pkg Package) error
}

type MongoCollection struct {
	session    *mgo.Session
	collection *mgo.Collection
}

func NewMongoCollection(host, db string) (*MongoCollection, error) {
	m := MongoCollection{}

	session, err := mgo.Dial(host)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %s", err)
	}
	if err := session.Ping(); err != nil {
		return nil, fmt.Errorf("database ping failed: %s", err)
	}

	m.collection = session.DB(db).C("packages")
	return &m, nil
}

func (c *MongoCollection) Close() error {
	c.session.Close()
	return nil
}

func (c *MongoCollection) Insert(pkg Package) error {
	return c.collection.Insert(pkg)
}

type MemoryCollection struct {
	Packages map[string]Package
}

func NewMemoryCollection() *MemoryCollection {
	return &MemoryCollection{make(map[string]Package)}
}

func (c *MemoryCollection) Insert(pkg Package) error {
	c.Packages[pkg.ImportPath] = pkg
	return nil
}

func (c *MemoryCollection) Dump() ([]byte, error) {
	return json.MarshalIndent(c.Packages, "", "\t")
}
