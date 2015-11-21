package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"regexp/syntax"

	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/zach-klippenstein/goregen"
	"golang.org/x/net/trace"
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
	Count int

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

	http.Handle("/", router)

	log.Printf("listening on :%d…\n", *ListenPort)
	log.Println(http.ListenAndServe(fmt.Sprintf(":%d", *ListenPort), nil))
}

type Request struct {
	*http.Request

	tr  trace.Trace
	log *log.Logger
}

func WrapRequest(req *http.Request, traceFamily string) *Request {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}

	prefix := fmt.Sprintf("%s - %s - ", host, traceFamily)
	return &Request{
		Request: req,
		tr:      trace.New(traceFamily, req.URL.Path),
		log:     log.New(os.Stdout, prefix, log.LstdFlags),
	}
}

func (r *Request) Finish() {
	r.tr.Finish()
	r.log.Println("request finished.")
}

func getHtml(w http.ResponseWriter, r *http.Request) {
	req := WrapRequest(r, "get.html")
	req.log.Println("handling html request")
	defer req.Finish()

	templ, err := template.ParseFiles("assets/index.html")
	if err != nil {
		logError(req, err.Error())
		http.Error(w, "error parsing template index.html", http.StatusInternalServerError)
		return
	}

	data := OutputData{
		MinCount:    1,
		MaxCount:    MaxOutputCount,
		AnalyticsID: *AnalyticsID,
	}
	defer templ.Execute(w, &data)

	input, err := parseRequest(req)
	data.InputData = input

	if err != nil {
		data.ErrorMsg = err.Error()
		logError(req, fmt.Sprintln("error parsing request:", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if input.Regex == "" {
		data.Suggestion, data.SuggestionUrl = generateSuggestion()
		return
	}

	results, err := generateOutput(req, input)
	if err != nil {
		data.ErrorMsg = err.Error()
		logError(req, fmt.Sprintln("error generating output:", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	data.Results = results
	return
}

func getJson(w http.ResponseWriter, r *http.Request) {
	req := WrapRequest(r, "get.json")
	req.log.Println("handling json request")
	defer req.Finish()

	input, err := parseRequest(req)
	if err != nil {
		logError(req, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	results, err := generateOutput(req, input)
	if err != nil {
		logError(req, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func parseRequest(req *Request) (input InputData, err error) {
	if err = req.ParseForm(); err != nil {
		return
	}
	inputDecoder.Decode(&input, req.Form)
	input.Count = sanitizeCount(input.Count)

	req.log.Printf("got form data: %#v", input)
	req.tr.LazyPrintf("Input=%#v", input)

	return
}

func generateOutput(req *Request, input InputData) (results []string, err error) {
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
			req.log.Printf("generating %d outputs...", input.Count)
			for i := 0; i < input.Count; i++ {
				results = append(results, gen.Generate())
			}
		}
	}

	return
}

func logError(req *Request, msg string) {
	req.log.Println(msg)
	req.tr.LazyPrintf("%s", msg)
	req.tr.SetError()
}

func sanitizeCount(count int) int {
	if count > MaxOutputCount {
		count = MaxOutputCount
	} else if count <= 0 {
		count = DefaultOutputCount
	}
	return count
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
