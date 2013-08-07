// fetch retrieves a list of packages from the given source
package main

import (
	"flag"
	"fmt"
	"github.com/kisielk/gosrc"
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	flag.Parse()
	source := flag.Arg(0)

	var (
		packages []string
		err      error
	)

	switch source {
	case "godoc":
		packages, err = gosrc.GodocPackages()
	case "":
		log.Fatalf("usage: %s [source]", os.Args[0])
	default:
		log.Fatalln("unknown source:", source)
	}

	if err != nil {
		log.Fatal(err)
	}

	for _, pkg := range packages {
		fmt.Println(pkg)
	}
}
