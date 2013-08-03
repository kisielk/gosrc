package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	gopath      = "./gopath"
	numBuilders = 8
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

func getPackages(gopath string, pkgs []string) {
	pkgChan := make(chan string)
	for i := 0; i < numBuilders; i++ {
		go builder(gopath, pkgChan)
	}
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

func getPackage(gopath, pkg string) {
	log.Print("building ", pkg)
	cmd := exec.Command("go", "get", "-u", pkg)
	cmd.Env = makeEnv(gopath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Println("failed: ", err)
	} else {
		log.Println("success")
	}
}

func builder(goroot string, pkgs chan string) {
	for pkg := range pkgs {
		getPackage(goroot, pkg)
	}
}

func main() {
	gopath, err := filepath.Abs(gopath)
	if err != nil {
		log.Fatal("failed to determine GOPATH:", err)
	}

	world, err := getWorld()
	if err != nil {
		log.Fatal("failed to get package list", err)
	}

	getPackages(gopath, world)
}
