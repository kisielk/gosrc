package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"github.com/kisielk/gosrc"
	"go/build"
	"labix.org/v2/mgo"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	gopath      = flag.String("gopath", filepath.Join(os.TempDir(), "gopath"), "GOPATH to use for builds")
	numBuilders = flag.Int("builders", 8, "Number of concurrent builders")
	mongo       = flag.String("mongo", "localhost", "MongoDB host")
	database    = flag.String("database", "test", "MongoDB database")
)

var (
	goroot       = filepath.Clean(runtime.GOROOT())
	gorootSrcPkg = filepath.Join(goroot, "src/pkg")
)

// stdPackages is a list of package names found in the standard library
var stdPackages = func() []string {
	var pkgs []string
	filepath.Walk(gorootSrcPkg, func(path string, fi os.FileInfo, err error) error {
		if err != nil || !fi.IsDir() || path == gorootSrcPkg {
			return nil
		}
		relPath, err := filepath.Rel(gorootSrcPkg, path)
		if err != nil {
			return err
		}
		pkgs = append(pkgs, relPath)
		return nil
	})
	return pkgs
}()

func isStd(pkg string) bool {
	for _, p := range stdPackages {
		if p == pkg {
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

	for _, result := range w.Results {
		if !isStd(result.Path) {
			world = append(world, result.Path)
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
	for _, v := range AllVCS {
		repo.Revision = v.Revision(path)
		if repo.Revision.Id == "" {
			continue
		}
		repo.Type = v.Name()
		repo.Root = v.Root(path)
		repo.URL = v.URL(path)
		break
	}
	path, err := filepath.Rel(filepath.Join(gopath, "src"), repo.Root)
	if err != nil {
		path = repo.Root
	}
	repo.Root = path
	return repo
}

func goGet(gopath, pkg string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("go", "get", "-u", pkg)
	cmd.Env = makeEnv(gopath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func goTest(gopath, pkg string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("go", "test", pkg)
	cmd.Env = makeEnv(gopath)
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return out.String(), err
}

func goVet(gopath, pkg string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("go", "vet", pkg)
	cmd.Env = makeEnv(gopath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func getPackage(gopath, pkg string) gosrc.Package {
	p := gosrc.Package{
		ImportPath: pkg,
		Date:       time.Now(),
	}

	log.Println(pkg, "building")
	buildOut, err := goGet(gopath, pkg)
	p.Build.Log = buildOut
	if err != nil {
		log.Println(pkg, " build failed:", err)
	} else {
		log.Println(pkg, " build succeeded")
		p.Build.Succeeded = true

		log.Println(pkg, "testing")
		testOut, err := goTest(gopath, pkg)
		if err != nil {
			log.Println(pkg, "testing failed:", err)
		} else {
			log.Println(pkg, "testing succeeded")
			p.Test.Succeeded = true
		}
		p.Test.Log = testOut

		log.Println(pkg, "vetting")
		vetOut, err := goVet(gopath, pkg)
		if err != nil {
			log.Println(pkg, "vetting failed:", err)
		} else {
			log.Println(pkg, "vetting succeeded")
			p.Vet.Errors = strings.Count(vetOut, "\n")
		}
		p.Vet.Log = vetOut

		p.Imports = getImports(gopath, pkg)
	}
	p.Repository = getRepository(gopath, pkg)
	return p
}

func getImports(gopath, pkg string) []string {
	var imports []string
	ctx := build.Default
	ctx.GOPATH = gopath
	ctx.UseAllFiles = true
	buildPkg, err := ctx.Import(pkg, "", 0)
	if err != nil {
		log.Println(pkg, "couldn't import:", err)
		return imports
	}

	for _, imp := range buildPkg.Imports {
		if !isStd(imp) {
			imports = append(imports, imp)
		}
	}
	return imports
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
