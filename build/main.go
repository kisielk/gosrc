package main

import (
	"encoding/json"
	"flag"
	"github.com/kisielk/gosrc"
	"labix.org/v2/mgo"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	gopath      = flag.String("gopath", "./gopath", "GOPATH to use for builds")
	numBuilders = flag.Int("builders", 8, "Number of concurrent builders")
	mongo       = flag.String("mongo", "localhost", "MongoDB host")
	database    = flag.String("database", "test", "MongoDB database")
)

var prefixes = []string{
	"launchpad.net",
	"github.com",
	"code.google.com",
	"bitbucket.org",
}

func validPrefix(s string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func getWorld() ([]string, error) {
	var world []string
	resp, err := http.Get("http://api.godoc.org/packages")
	if err != nil {
		return world, err
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
		return world, err
	}
	log.Printf("found %d packages", len(w.Results))

	for _, path := range w.Results {
		p := path.Path
		if validPrefix(p) {
			world = append(world, p)
		}
	}
	log.Printf("%d packages after filtering", len(world))
	return world, nil
}

func getPackages(collection *mgo.Collection, gopath string, pkgs []string) {
	pkgChan := make(chan string)
	results := make(chan gosrc.Package)
	for i := 0; i < *numBuilders; i++ {
		go builder(gopath, pkgChan, results)
	}
	go func() {
		for p := range results {
			collection.Insert(p)
		}
	}()
	for _, p := range pkgs {
		pkgChan <- p
	}
}

func makeEnv(gopath string) []string {
	env := []string{"GOPATH=" + gopath}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GOPATH=") {
			env = append(env, e)
		}
	}
	return env
}

func getRepository(gopath, pkg string) gosrc.Repository {
	path := filepath.Join(gopath, "src", pkg)
	var repo gosrc.Repository
	switch {
	case strings.HasPrefix(pkg, "github.com"):
		repo.Type = "git"
		repo.Revision = git.Revision(path)
		repo.Root = git.Root(path)
	case strings.HasPrefix(pkg, "bitbucket.org") || strings.HasPrefix(pkg, "code.google.com"):
		repo.Type = "hg"
		repo.Revision = hg.Revision(path)
		if repo.Revision == "" {
			repo.Type = "git"
			repo.Revision = git.Revision(path)
			repo.Root = git.Root(path)
		} else {
			repo.Root = hg.Root(path)
		}
	case strings.HasPrefix(pkg, "launchpad.net"):
		repo.Type = "bzr"
		repo.Revision = bzr.Revision(path)
		repo.Root = bzr.Root(path)
	}
	path, err := filepath.Rel(filepath.Join(gopath, "src"), repo.Root)
	if err != nil {
		path = repo.Root
	}
	repo.Root = path
	return repo
}

func getPackage(gopath, pkg string) gosrc.Package {
	var (
		build = false
		test  = false
		env   = makeEnv(gopath)
		date  = time.Now()
	)
	log.Print("building ", pkg)
	cmd := exec.Command("go", "get", "-u", pkg)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()

	if err != nil {
		log.Println("build failed: ", err)
	} else {
		log.Println("build success")
		build = true
		log.Print("testing", pkg)
		cmd := exec.Command("go", "test", pkg)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			log.Println("testing failed: ", err)
		} else {
			log.Println("testing success")
			test = true
		}
	}
	repository := getRepository(gopath, pkg)

	return gosrc.Package{
		Path:       pkg,
		Build:      build,
		Test:       test,
		Date:       date,
		Repository: repository,
	}
}

func builder(goroot string, pkgs chan string, results chan gosrc.Package) {
	for pkg := range pkgs {
		results <- getPackage(goroot, pkg)
	}
}

func main() {
	session, err := mgo.Dial(*mongo)
	if err != nil {
		log.Fatal("failed to connect to database", err)
	}
	defer session.Close()
	if err := session.Ping(); err != nil {
		log.Fatal("database ping failed: ", err)
	}

	gopath, err := filepath.Abs(*gopath)
	if err != nil {
		log.Fatal("failed to determine GOPATH:", err)
	}

	world, err := getWorld()
	if err != nil {
		log.Fatal("failed to get package list", err)
	}

	collection := session.DB(*database).C("packages")
	getPackages(collection, gopath, world)
}
