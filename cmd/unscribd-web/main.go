package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
)

var pageTemplate *template.Template

func main() {
	var err error
	pageTemplate, err = template.ParseFiles("web/templates/index.html")
	if err != nil {
		panic(err)
	}
	root := os.Getenv("UNSCRIBD_WEB_OUTPUT_DIR")
	if root == "" {
		root = "/tmp/unscribd-downloads"
	}
	_ = os.MkdirAll(root, 0755)
	manager := NewManager(root)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	fmt.Println("listening on :" + port)
	if err := http.ListenAndServe(":"+port, newHandler(manager)); err != nil {
		panic(err)
	}
}
