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
	downloadQueue := make(chan string)
	buildQueue := make(chan string)

	downloadResults := startDownloader(gopath, pkgs, downloadQueue)
	buildResults := startBuilders(gopath, *numBuilders, buildQueue)

	downloading := len(pkgs)
	for {
		select {
		case r := <-downloadResults:
			downloading--
			if r.err != nil {
				log.Println(r.pkg, "failed to download:", r.err)
			} else {
				log.Println(r.pkg, "downloaded")
				buildQueue <- r.pkg
			}
		case r := <-buildResults:
			err := collection.Insert(r)
			if err != nil {
				log.Println(r, "failed to insert results:", err)
				continue
			}

			for _, imp := range r.BuildInfo.Imports {
				downloadQueue <- imp
			}
		}
	}
}

type downloadResult struct {
	pkg string
	err error
}

func startDownloader(gopath string, pkgs []string, downloadQueue chan string) chan downloadResult {
	downloadRequests := make(chan string)

	go func() {
		queue := newOneTimeQueue()
		for _, p := range pkgs {
			queue.Push(p)
		}

		var next string
		for {
			if next == "" {
				next = queue.Pop()
			}

			if next != "" {
				select {
				case p := <-downloadQueue:
					queue.Push(p)
				case downloadRequests <- next:
					next = ""
				}
			} else {
				p := <-downloadQueue
				queue.Push(p)
			}
		}
	}()
	return downloader(gopath, downloadRequests)
}

func downloader(gopath string, pkgs chan string) chan downloadResult {
	results := make(chan downloadResult)
	go func() {
		for pkg := range pkgs {
			log.Println(pkg, "downloading")
			err := download(gopath, pkg)
			results <- downloadResult{pkg, err}
		}
	}()
	return results
}

type oneTimeQueue struct {
	queue map[string]bool
	seen  map[string]bool
}

func (q *oneTimeQueue) Push(s string) {
	if !q.seen[s] {
		q.queue[s] = true
		q.seen[s] = true
	}
}

func (q *oneTimeQueue) Pop() string {
	for next := range q.queue {
		delete(q.queue, next)
		return next
	}
	return ""
}

func newOneTimeQueue() *oneTimeQueue {
	return &oneTimeQueue{make(map[string]bool), make(map[string]bool)}
}

func startBuilders(gopath string, builders int, buildQueue chan string) chan gosrc.Package {
	buildRequests := make(chan string)
	buildResults := make(chan gosrc.Package)

	for i := 0; i < builders; i++ {
		go builder(gopath, buildRequests, buildResults)
	}

	go func() {
		queue := newOneTimeQueue()

		var next string
		for {
			if next == "" {
				next = queue.Pop()
			}

			if next != "" {
				select {
				case pkg := <-buildQueue:
					queue.Push(pkg)
				case buildRequests <- next:
					next = ""
				}
			} else {
				pkg := <-buildQueue
				queue.Push(pkg)
			}
		}
	}()
	return buildResults
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
	cmd.Stderr = os.Stderr
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
		out, _ := c.Dump()
		log.Println("result:", out)
	}
}
