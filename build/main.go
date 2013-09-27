package main

import (
	"bytes"
	"flag"
	"github.com/kisielk/gosrc"
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var (
	gopath      = flag.String("gopath", filepath.Join(os.TempDir(), "gosrc/gopath"), "GOPATH to use for builds")
	numBuilders = flag.Int("builders", 8, "Number of concurrent builders")
	mongo       = flag.String("mongo", "", "MongoDB host")
	database    = flag.String("database", "test", "MongoDB database")
)

var (
	goroot       = filepath.Clean(runtime.GOROOT())
	gorootSrcPkg = filepath.Join(goroot, "src/pkg")
)

// isStd is a boolean map of packages in the Go standard library
var isStd = func() map[string]bool {
	pkgs := make(map[string]bool)
	filepath.Walk(gorootSrcPkg, func(path string, fi os.FileInfo, err error) error {
		if err != nil || !fi.IsDir() || path == gorootSrcPkg {
			return nil
		}
		relPath, err := filepath.Rel(gorootSrcPkg, path)
		if err != nil {
			return err
		}
		pkgs[relPath] = true
		return nil
	})
	return pkgs
}()

func getPackages(collection gosrc.Collection, gopath string, pkgs []string) {
	var (
		buildRequests   = make(chan string)
		buildResults    = make(chan gosrc.Package)
		downloadResults = make(chan downloadResult)
	)

	go downloader(gopath, pkgs, downloadResults)
	go func() {
		for {
			var (
				next    string
				queue   = make(map[string]bool)
				visited = make(map[string]bool)
			)

			// Pop a random package from the queue
			for p := range queue {
				next = p
				break
			}

			storePackage := func(result gosrc.Package) {
				err := collection.Insert(result)
				if err != nil {
					log.Println(result, "failed to insert results:", err)
					queue[result.ImportPath] = true
					visited[result.ImportPath] = false
					return
				}

				for _, imp := range result.BuildInfo.Imports {
					if !visited[imp] {
						queue[imp] = true
					}
				}
			}

			queuePackage := func(result downloadResult) {
				if result.err != nil {
					log.Println(result.pkg, "failed to download:", result.err)
				} else {
					queue[result.pkg] = true
				}
			}

			if next != "" {
				select {
				case result := <-downloadResults:
					queuePackage(result)
				case result := <-buildResults:
					storePackage(result)
				case buildRequests <- next:
					// if a builder has accepted the package pop it from the queue
					// and add it to the visited list.
					delete(queue, next)
					visited[next] = true
				}
			} else {
				select {
				case result := <-downloadResults:
					queuePackage(result)
				case result := <-buildResults:
					storePackage(result)
				}
			}

		}
	}()
	for i := 0; i < *numBuilders; i++ {
		go builder(gopath, buildRequests, buildResults)
	}
	select {}
}

type downloadResult struct {
	pkg string
	err error
}

func downloader(gopath string, pkgs []string, results chan downloadResult) {
	for _, pkg := range pkgs {
		log.Println(pkg, "downloading")
		err := download(gopath, pkg)
		if err != nil {
			log.Println(pkg, "failed to download:", err)
		} else {
			log.Println(pkg, "downloaded")
		}
		results <- downloadResult{pkg, err}
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

func goFmt(gopath, pkg string) (int, error) {
	var out bytes.Buffer
	cmd := exec.Command("gofmt", "-l", filepath.Join(gopath, "src", pkg))
	cmd.Env = makeEnv(gopath)
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	n := bytes.Count(out.Bytes(), []byte("\n"))
	return n, err
}

func download(gopath, pkg string) error {
	cmd := exec.Command("go", "get", "-d", "-u", pkg)
	cmd.Env = makeEnv(gopath)
	return cmd.Run()
}

func buildPkg(gopath, pkg string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("go", "get", pkg)
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

func errcheck(gopath, pkg string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("errcheck", pkg)
	cmd.Env = makeEnv(gopath)
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if e1, ok := err.(*exec.ExitError); ok && exitStatus(e1) == 1 {
		// errcheck returns 1 if there were errors found
		err = nil
	}
	return out.String(), err
}

// exitStatus extracts the exit status from an ExitError
func exitStatus(err *exec.ExitError) int {
	return err.Sys().(syscall.WaitStatus).ExitStatus()

}

func importPkg(gopath, pkg string) *build.Package {
	ctx := build.Default
	ctx.GOPATH = gopath
	ctx.UseAllFiles = true
	buildPkg, err := ctx.Import(pkg, "", 0)
	if err != nil {
		log.Println(pkg, "couldn't import:", err)
		return nil
	}
	return buildPkg
}

func getPackage(gopath, pkg string) gosrc.Package {
	p := gosrc.Package{
		ImportPath: pkg,
		Date:       time.Now(),
	}

	log.Println(pkg, "importing")
	impPkg := importPkg(gopath, pkg)
	if impPkg == nil || impPkg.Goroot {
		return p
	}
	p.BuildInfo = gosrc.NewBuildInfo(impPkg)

	log.Println(pkg, "building")
	buildOut, err := buildPkg(gopath, pkg)
	p.Build.Log = buildOut
	if err != nil {
		log.Println(pkg, "build failed:", err)
	} else {
		log.Println(pkg, "build succeeded")
		p.Build.Succeeded = true

		log.Println(pkg, "gofmt")
		n, err := goFmt(gopath, pkg)
		if err != nil {
			log.Println(pkg, "gofmt failed:", err)
		} else {
			log.Println(pkg, "gofmt succeeded")
			p.Gofmt.Differences = n
		}

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

		log.Println(pkg, "errchecking")
		errcheckOut, err := errcheck(gopath, pkg)
		if err != nil {
			log.Println(pkg, "errcheck failed:", err)
		} else {
			log.Println(pkg, "errcheck succeeded")
			p.Errcheck.Errors = strings.Count(errcheckOut, "\n")
		}
		p.Errcheck.Log = errcheckOut

	}
	p.Repository = getRepository(gopath, pkg)
	return p
}

func builder(gopath string, pkgs chan string, results chan gosrc.Package) {
	for pkg := range pkgs {
		results <- getPackage(gopath, pkg)
	}
}

func main() {
	flag.Parse()
	packages := flag.Arg(0)
	if packages == "" {
		log.Fatalf("usage: %s [package list file]", os.Args[0])
	}

	pkgList, err := gosrc.FilePackages(packages)
	if err != nil {
		log.Fatalln("failed to read packages:", err)
	}

	gopath, err := filepath.Abs(*gopath)
	if err != nil {
		log.Fatalln("failed to determine GOPATH:", err)
	}

	var collection gosrc.Collection
	if *mongo != "" {
		var err error
		collection, err = gosrc.NewMongoCollection(*mongo, *database)
		if err != nil {
			log.Fatalln("failed to connect to MongoDB:", err)
		}
	} else {
		collection = gosrc.NewMemoryCollection()
	}

	getPackages(collection, gopath, pkgList)

	if *mongo == "" {
		c := collection.(*gosrc.MemoryCollection)
		log.Println(c.Dump())
	}
}
