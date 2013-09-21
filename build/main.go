package main

import (
	"bufio"
	"bytes"
	"flag"
	"github.com/kisielk/gosrc"
	"go/build"
	"io"
	"labix.org/v2/mgo"
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
	gopath      = flag.String("gopath", filepath.Join(os.TempDir(), "gopath"), "GOPATH to use for builds")
	numBuilders = flag.Int("builders", 8, "Number of concurrent builders")
	mongo       = flag.String("mongo", "localhost", "MongoDB host")
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

func getPackages(collection *mgo.Collection, gopath string, pkgs []string) {
	var (
		pkgChan = make(chan string)
		results = make(chan gosrc.Package)
		queue   = make(map[string]bool)
		visited = make(map[string]bool)
	)

	for i := 0; i < *numBuilders; i++ {
		go builder(gopath, pkgChan, results)
	}

	for _, p := range pkgs {
		queue[p] = true
	}

	for {
		// pick a pseudo-random package name from the queue.
		for p := range queue {
			select {
			case result := <-results:
				collection.Insert(result)
				for _, imp := range result.BuildInfo.Imports {
					if !visited[imp] {
						queue[imp] = true
					}
				}
			case pkgChan <- p:
				// if a builder has accepted the package pop it from the queue
				// and add it to the visited list.
				delete(queue, p)
				visited[p] = true
			}

			// restart the loop so we select a new pseudo-random package.
			break
		}
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

		log.Println(pkg, "errchecking")
		errcheckOut, err := errcheck(gopath, pkg)
		if err != nil {
			log.Println(pkg, "errcheck failed:", err)
		} else {
			log.Println(pkg, "errcheck succeeded")
			p.Errcheck.Errors = strings.Count(errcheckOut, "\n")
		}
		p.Errcheck.Log = errcheckOut

		p.BuildInfo = getBuildInfo(gopath, pkg)
	}
	p.Repository = getRepository(gopath, pkg)
	return p
}

func getBuildInfo(gopath, pkg string) gosrc.BuildInfo {
	var buildInfo gosrc.BuildInfo
	ctx := build.Default
	ctx.GOPATH = gopath
	ctx.UseAllFiles = true
	buildPkg, err := ctx.Import(pkg, "", 0)
	if err != nil {
		log.Println(pkg, "couldn't import:", err)
		return buildInfo
	}

	return gosrc.BuildInfo{
		Imports: buildPkg.Imports,
		UsesCgo: len(buildPkg.CgoFiles) > 0,
		GoFiles: buildPkg.GoFiles,
	}
}

func builder(goroot string, pkgs chan string, results chan gosrc.Package) {
	for pkg := range pkgs {
		results <- getPackage(goroot, pkg)
	}
}

func readLines(src io.Reader) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func main() {
	flag.Parse()
	packages := flag.Arg(0)
	if packages == "" {
		log.Fatalf("usage: %s [package list file]", os.Args[0])
	}

	file, err := os.Open(packages)
	if err != nil {
		log.Fatalln("failed to open packages file:", err)
	}
	pkgList, err := readLines(file)
	if err != nil {
		log.Fatalln("failed to read packages:", err)
	}

	session, err := mgo.Dial(*mongo)
	if err != nil {
		log.Fatalln("failed to connect to database:", err)
	}
	defer session.Close()
	if err := session.Ping(); err != nil {
		log.Fatalln("database ping failed:", err)
	}

	gopath, err := filepath.Abs(*gopath)
	if err != nil {
		log.Fatalln("failed to determine GOPATH:", err)
	}

	collection := session.DB(*database).C("packages")
	getPackages(collection, gopath, pkgList)
}
