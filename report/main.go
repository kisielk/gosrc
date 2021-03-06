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
	"path"
	"path/filepath"
)

var (
	mongo    = flag.String("mongo", "localhost", "MongoDB host")
	database = flag.String("database", "test", "MongoDB database")
	httpAddr = flag.String("http", ":8080", "HTTP listening address")
	gopath   = flag.String("gopath", "/tmp/gosrc/gopath", "GOPATH where build files are located")
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
<th>Errcheck</th>
<th>Revision</th>
<th>Repository</th>
</tr>
{{range .Packages}}
<tr>
<td><a href="/{{.ImportPath}}">{{.ImportPath}}</a></td>
<td>{{if .Build.Succeeded}}<span class="check">✔</span>{{else}}<span class="cross">✘</span>{{end}}</td>
<td>{{if .Test.Succeeded}}<span class="check">✔</span>{{else}}<span class="cross">✘</span>{{end}}</td>
<td>{{.Vet.Errors}}</td>
<td>{{.Errcheck.Errors}}</td>
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
<a href="/-/files/{{.ImportPath}}">Files</a>
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
<h2>Errcheck Log</h2>
<pre>
{{.Errcheck.Log}}
</pre>
<h2>Imports</h2>
<ul>
{{range .BuildInfo.Imports}}
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

const filesTemplate = `
<!DOCTYPE html>
<html>
<head>
<title> Files for {{.ImportPath}}</title>
<script src="//ajax.googleapis.com/ajax/libs/jquery/1.10.2/jquery.min.js"></script>
</head>
<body>
<div id="header">
<select id="files" name="file" onChange="load(this.value)">
{{range .BuildInfo.GoFiles}}
<option value="{{.}}">{{.}}</option>
{{end}}
</select>
</div>
<div id="content">
</div>
<script>
function load(file) {
	$("#content").load("/-/file/{{.ImportPath}}/" + file);
}
window.onload = function() {
	$("#files").change();	
}
</script>
</body>
</html>
`

var templates = map[string]*template.Template{
	"index":   parseTemplate("index", indexTemplate),
	"package": parseTemplate("package", packageTemplate),
	"repo":    parseTemplate("repo", repoTemplate),
	"files":   parseTemplate("files", filesTemplate),
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
	pkg, err := findPackage(req.URL.Path[1:])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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

func getFiles(w http.ResponseWriter, req *http.Request) {
	pkg, err := findPackage(req.URL.Path[len(filesPath):])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	err = templates["files"].Execute(w, pkg)
	if err != nil {
		log.Print(err)
	}
}

func getFile(w http.ResponseWriter, req *http.Request) {
	fileName := path.Base(req.URL.Path)
	pkgName := req.URL.Path[len(filePath) : len(req.URL.Path)-len(fileName)-1]
	pkg, err := findPackage(pkgName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	path := filepath.Join(*gopath, "src", pkg.ImportPath, fileName)
	http.ServeFile(w, req, path)
}

func findPackage(path string) (gosrc.Package, error) {
	c := session.DB(*database).C("packages")
	var pkg gosrc.Package
	err := c.Find(bson.M{"importpath": path}).One(&pkg)
	return pkg, err
}

const (
	indexPath = "/-/index"
	repoPath  = "/-/repo/"
	filesPath = "/-/files/"
	filePath  = "/-/file/"
)

func main() {
	s, err := mgo.Dial(*mongo)
	if err != nil {
		log.Fatalln("failed to connect to database:", err)
	}
	defer s.Close()
	if err := s.Ping(); err != nil {
		log.Fatalln("database ping failed:", err)
	}
	session = s

	http.HandleFunc(indexPath, getIndex)
	http.HandleFunc(repoPath, getRepo)
	http.HandleFunc(filesPath, getFiles)
	http.HandleFunc(filePath, getFile)
	http.HandleFunc("/", getPackage)
	err = http.ListenAndServe(*httpAddr, nil)
	if err != nil {
		log.Fatal(err)
	}
}
