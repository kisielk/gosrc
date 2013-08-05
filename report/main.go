package main

import (
	"flag"
	"github.com/kisielk/gosrc"
	"html/template"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	"log"
	"net/http"
	"net/url"
)

var (
	mongo    = flag.String("mongo", "localhost", "MongoDB host")
	database = flag.String("database", "test", "MongoDB database")
)

var session *mgo.Session

const indexTemplate = `
<!DOCTYPE html>
<html>
<head>
<title>Index</title>
<style>
.check {
	color: green
}

.cross {
	color: red
}
</style>
</head>
<body>
<table>
<tr>
<th>Import Path</th>
<th>Build</th>
<th>Test</th>
<th>Vet</th>
<th>Revision</th>
<th>Repository</th>
</tr>
{{range .Packages}}
<tr>
<td><a href="/{{.ImportPath}}">{{.ImportPath}}</a></td>
<td>{{if .Build.Succeeded}}<span class="check">✔</span>{{else}}<span class="cross">✘</span>{{end}}</td>
<td>{{if .Test.Succeeded}}<span class="check">✔</span>{{else}}<span class="cross">✘</span>{{end}}</td>
<td>{{.Vet.Errors}}</td>
<td>{{.Repository.Revision.Id | limit 10}}</td>
<td><a href="/-/repo?r={{.Repository.URL}}">{{.Repository.URL}}</a></td>
</tr>
{{end}}
</table>
</body>
</html>
`

const packageTemplate = `
<!DOCTYPE html>
<html>
<head>
<title>{{.ImportPath}}</title>
</head>
<body>
<h1>{{.ImportPath}}</h1>
<h2>Revision</h2>
{{with .Repository.Revision}}
<dl>
<dt>Id</dt>
<dd>{{.Id}}</dd>
<dt>Author</dt>
<dd>{{.Author}}</dd>
<dt>Date</dt>
<dd>{{.Date}}</dd>
</dl>
{{end}}
<h2>Build Log</h2>
<pre>
{{.Build.Log}}
</pre>
<h2>Test Log</h2>
<pre>
{{.Test.Log}}
</pre>
<h2>Vet Log</h2>
<pre>
{{.Vet.Log}}
</pre>
<h2>Imports</h2>
<ul>
{{range .Imports}}
<li><a href="/{{.}}">{{.}}</a></li>
{{end}}
</ul>
</body>
</html>
`

const repoTemplate = `
<!DOCTYPE html>
<html>
<head>
<title>Repo {{.URL}}</title>
</head>
<body>
<h1>Packages</h1>
<ul>
{{range .Packages}}
<li><a href="/{{.ImportPath}}">{{.ImportPath}}</a></li>
{{end}}
</ul>
</body>
</html>
`

var templates = map[string]*template.Template{
	"index":   parseTemplate("index", indexTemplate),
	"package": parseTemplate("package", packageTemplate),
	"repo":    parseTemplate("repo", repoTemplate),
}

func parseTemplate(name, t string) *template.Template {
	return template.Must(template.New(name).Funcs(funcMap).Parse(t))
}

var funcMap = template.FuncMap{
	"queryEscape": url.QueryEscape,
	"limit": func(n int, s string) string {
		runes := []rune(s)
		if n > len(runes) {
			n = len(runes)
		}
		return string(runes[:n])
	},
}

func getIndex(w http.ResponseWriter, req *http.Request) {
	collection := session.DB(*database).C("packages")
	var packages []gosrc.Package
	err := collection.Find(nil).Iter().All(&packages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = templates["index"].Execute(w, map[string]interface{}{"Packages": packages})
	if err != nil {
		log.Print(err)
	}
}

func getPackage(w http.ResponseWriter, req *http.Request) {
	c := session.DB(*database).C("packages")
	var pkg gosrc.Package
	path := req.URL.Path[1:]
	err := c.Find(bson.M{"importpath": path}).One(&pkg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = templates["package"].Execute(w, pkg)
	if err != nil {
		log.Print(err)
	}
}

func getRepo(w http.ResponseWriter, req *http.Request) {
	c := session.DB(*database).C("packages")
	repo := req.FormValue("r")
	var packages []gosrc.Package
	log.Println(repo)
	err := c.Find(bson.M{"repository.url": repo}).Iter().All(&packages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	err = templates["repo"].Execute(w, map[string]interface{}{"URL": repo, "Packages": packages})
	if err != nil {
		log.Print(err)
	}
}

const (
	indexPath = "/-/index"
	repoPath  = "/-/repo"
)

func main() {
	s, err := mgo.Dial(*mongo)
	if err != nil {
		log.Fatal("failed to connect to database", err)
	}
	defer s.Close()
	if err := s.Ping(); err != nil {
		log.Fatal("database ping failed: ", err)
	}
	session = s

	http.HandleFunc(indexPath, getIndex)
	http.HandleFunc(repoPath, getRepo)
	http.HandleFunc("/", getPackage)
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
