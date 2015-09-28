package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"

	"regexp/syntax"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/zach-klippenstein/goregen"
)

const (
	RegexSuggestion = `Hello,? (world|you( (fantastic|wonderful|amazing) (human|person|individual))?)[.!]`

	DefaultOutputCount = 5
	MaxOutputCount     = 1000
)

type CheckState bool

func (s CheckState) String() string {
	if s {
		return "checked"
	}
	return ""
}

func (s CheckState) GoString() string {
	if s {
		return "true"
	}
	return "false"
}

type InputData struct {
	// Set to the regex passed in RegexFieldName, or empty.
	Regex string

	// Number of results to generate.
	Count uint

	// regexp.syntax flags.
	FoldCase  CheckState
	ClassNL   CheckState
	DotNL     CheckState
	OneLine   CheckState
	NonGreedy CheckState
	PerlX     CheckState
}

var inputDecoder = schema.NewDecoder()

type OutputData struct {
	InputData

	// Used to provide an example regex to use if no regex is specified.
	// Either both will be empty, or both non-empty.
	Suggestion    string
	SuggestionUrl string

	MinCount uint
	MaxCount uint

	// If Regex could not be parsed, contains the error message.
	ErrorMsg string

	// If Regex could be parsed, contains the results of running the generator.
	Results []string

	AnalyticsID string
}

var (
	ListenPort  = flag.Uint("port", 8080, "port to listen on")
	AnalyticsID = flag.String("analytics-id", "", "optional ID to use for analytics tracking")
)

var router *mux.Router

func main() {
	flag.Parse()

	router = mux.NewRouter()

	router.HandleFunc("/", getJson).
		Methods("GET").
		HeadersRegexp("Accept", "application/json")

	router.HandleFunc("/", getHtml).
		Methods("GET").
		Name("query")

	loggedRouter := handlers.LoggingHandler(os.Stdout, router)
	http.Handle("/", loggedRouter)

	log.Println(http.ListenAndServe(fmt.Sprintf(":%d", *ListenPort), nil))
}

func getHtml(w http.ResponseWriter, req *http.Request) {
	log.Println("handling html request")

	templ, err := template.ParseFiles("assets/index.html")
	if err != nil {
		log.Println(err)
		http.Error(w, "error parsing index.html", http.StatusInternalServerError)
		return
	}

	data := OutputData{
		MinCount:    1,
		MaxCount:    MaxOutputCount,
		AnalyticsID: *AnalyticsID,
	}

	input, results, err := generateOutput(req)
	data.InputData = input
	if err != nil {
		data.ErrorMsg = err.Error()
	} else if input.Regex == "" {
		data.Suggestion, data.SuggestionUrl = generateSuggestion()
	} else {
		data.Results = results
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

func generateOutput(req *http.Request) (input InputData, results []string, err error) {
	if err = req.ParseForm(); err != nil {
		return
	}
	inputDecoder.Decode(&input, req.Form)
	log.Printf("got form data: %#v", input)

	if input.Regex != "" {
		var gen regen.Generator
		var args regen.GeneratorArgs

		if input.FoldCase {
			args.Flags |= syntax.FoldCase
		}
		if input.ClassNL {
			args.Flags |= syntax.ClassNL
		}
		if input.DotNL {
			args.Flags |= syntax.DotNL
		}
		if input.OneLine {
			args.Flags |= syntax.OneLine
		}
		if input.NonGreedy {
			args.Flags |= syntax.NonGreedy
		}
		if input.PerlX {
			args.Flags |= syntax.PerlX
		}

		gen, err = regen.NewGenerator(input.Regex, &args)
		if err != nil {
			return
		} else {
			count := sanitizeCount(&input.Count)
			log.Printf("generating %d outputs...", count)
			for i := 0; i < count; i++ {
				results = append(results, gen.Generate())
			}
		}
	}

	return
}

func sanitizeCount(count *uint) int {
	if *count > MaxOutputCount {
		*count = MaxOutputCount
	} else if *count == 0 {
		*count = DefaultOutputCount
	}
	return int(*count)
}

func generateSuggestion() (regex, queryUrlString string) {
	if queryUrl, err := router.Get("query").URLPath(); err == nil {
		regex = RegexSuggestion
		values := url.Values{}
		values.Set("Regex", regex)
		queryUrl.RawQuery = values.Encode()
		queryUrlString = queryUrl.String()
	}
	return
}
