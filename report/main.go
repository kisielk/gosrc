package main

import (
	"flag"
	"github.com/kisielk/gosrc"
	"html/template"
	"labix.org/v2/mgo"
	"log"
	"net/http"
)

var (
	mongo    = flag.String("mongo", "localhost", "MongoDB host")
	database = flag.String("database", "test", "MongoDB database")
)

var session *mgo.Session

const indexTemplateText = `
<!DOCTYPE html>
<html>
<head>
<title>Index</title>
</head>
<body>
<table>
<tr><td>Import Path</td><td>Build</td><td>Test</td></tr>
{{range .Packages}}
<tr>
<td>{{.Path}}</td>
<td>{{.Build}}</td>
<td>{{.Test}}</td>
</tr>
{{end}}
</table>
</body>
</html>
`

var indexTemplate = template.Must(template.New("index").Parse(indexTemplateText))

func index(w http.ResponseWriter, req *http.Request) {
	collection := session.DB(*database).C("packages")
	var packages []gosrc.Package
	err := collection.Find(nil).Iter().All(&packages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	indexTemplate.Execute(w, map[string]interface{}{"Packages": packages})
}

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

	http.HandleFunc("/index", index)
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
