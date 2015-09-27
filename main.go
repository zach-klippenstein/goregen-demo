package main

import (
	"encoding/json"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/zach-klippenstein/goregen"
"fmt"
)

const (
	RegexFieldName = "regex"

	DefaultOutputCount = 5
	MaxOutputCount     = 100
)

type Data struct {
	// Name of the query field for the regex.
	RegexFieldName string

	// Set to the regex passed in RegexFieldName, or empty.
	Regex string

	// If Regex could not be parsed, contains the error message.
	ErrorMsg string

	// If Regex could be parsed, contains the results of running the generator.
	Outputs []string
}

var (
	ListenPort = flag.Uint("port", 8080, "port to listen on")
)

func main() {
	flag.Parse()

	router := mux.NewRouter()

	router.HandleFunc("/", getJson).
		Methods("GET").
		HeadersRegexp("Accept", "application/json")

	router.HandleFunc("/", getHtml).
		Methods("GET")

	loggedRouter := handlers.LoggingHandler(os.Stdout, router)
	http.ListenAndServe(fmt.Sprintf(":%d", *ListenPort), loggedRouter)
}

func getHtml(w http.ResponseWriter, req *http.Request) {
	log.Println("handling html request")

	templ, err := template.ParseFiles("assets/index.html")
	if err != nil {
		log.Println(err)
		http.Error(w, "error parsing index.html", http.StatusInternalServerError)
		return
	}

	data := Data{
		RegexFieldName: RegexFieldName,
	}

	regex, results, err := generateOutput(req)
	if err != nil {
		data.ErrorMsg = err.Error()
	} else {
		data.Regex = regex
		data.Outputs = results
	}

	templ.Execute(w, &data)
}

func getJson(w http.ResponseWriter, req *http.Request) {
	log.Println("handling json request")

	_, results, err := generateOutput(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func generateOutput(req *http.Request) (regex string, results []string, err error) {
	if regex = req.FormValue(RegexFieldName); regex != "" {
		log.Printf("got regex: /%s/", regex)

		var gen regen.Generator
		gen, err = regen.NewGenerator(regex, &regen.GeneratorArgs{})
		if err != nil {
			return
		} else {
			count := getCountOrDefault(req)
			var i uint
			log.Printf("generating %d outputs...", count)
			for i = 0; i < count; i++ {
				results = append(results, gen.Generate())
			}
		}
	}

	return
}

func getCountOrDefault(req *http.Request) uint {
	rawCount := req.FormValue("count")
	if rawCount != "" {
		count, err := strconv.ParseUint(rawCount, 0, 32)
		if err != nil {
			log.Println("invalid count:", rawCount)
			return DefaultOutputCount
		}
		if count > MaxOutputCount {
			return MaxOutputCount
		} else if count == 0 {
			return 1
		}
		return uint(count)
	}

	return DefaultOutputCount
}
