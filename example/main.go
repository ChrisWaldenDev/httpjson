// Command example is a small API showing how to use httpjson.
//
//	go run ./example
//	curl localhost:8080/api/health
//	curl localhost:8080/api/items/1
//	curl -X POST localhost:8080/api/items -d '{"name":"Wrench"}'
//	curl localhost:8080/api/error
package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/ChrisWaldenDev/httpjson"
)

func main() {
	log.Fatal(http.ListenAndServe(":8080", Routes()))
}

// Routes returns the handler for every API endpoint.
func Routes() http.Handler {
	s := newServer()
	mx := http.NewServeMux()

	mx.HandleFunc("GET /api/health", s.health)
	mx.HandleFunc("GET /api/items/{id}", s.getItem)
	mx.HandleFunc("POST /api/items", s.createItem)
	mx.HandleFunc("GET /api/error", s.unexpectedError)

	return mx
}

// Item is the resource this API serves.
type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// errNotFound stands in for an error from your own storage code.
var errNotFound = errors.New("item not found")

// server keeps items in memory. In a real app this would be a database.
// The mutex is needed because net/http runs handlers concurrently.
type server struct {
	mu     sync.Mutex
	items  map[int]Item
	nextID int
}

func newServer() *server {
	return &server{
		items:  map[int]Item{1: {ID: 1, Name: "Hammer"}},
		nextID: 2,
	}
}

func (s *server) findItem(id int) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Item{}, errNotFound
	}
	return item, nil
}

func (s *server) addItem(name string) Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := Item{ID: s.nextID, Name: name}
	s.items[item.ID] = item
	s.nextID++
	return item
}

// health shows Write.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
}

// getItem shows Errorf turning a bad path value into a 400 and a not-found
// error into a 404, and Write sending the item.
func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		httpjson.WriteError(w, httpjson.Errorf(http.StatusBadRequest, "item id must be a number"))
		return
	}
	item, err := s.findItem(id)
	if errors.Is(err, errNotFound) {
		httpjson.WriteError(w, httpjson.Errorf(http.StatusNotFound, "item %d not found", id))
		return
	}
	if err != nil {
		httpjson.WriteError(w, err) // logged, client sees a 500
		return
	}
	httpjson.Write(w, http.StatusOK, item)
}

type createItemRequest struct {
	Name string `json:"name"`
}

// createItem shows Decode reading the body, Errorf for validation, and
// Write sending a 201.
func (s *server) createItem(w http.ResponseWriter, r *http.Request) {
	var req createItemRequest
	if err := httpjson.Decode(w, r, &req); err != nil {
		httpjson.WriteError(w, err) // already a 400 or 413
		return
	}
	if req.Name == "" {
		httpjson.WriteError(w, httpjson.Errorf(http.StatusBadRequest, "name is required"))
		return
	}
	httpjson.Write(w, http.StatusCreated, s.addItem(req.Name))
}

// unexpectedError shows WriteError with an error that isn't an *httpjson.Error,
// like a database failure: it's logged, and the client gets a generic 500.
func (s *server) unexpectedError(w http.ResponseWriter, r *http.Request) {
	httpjson.WriteError(w, errors.New("database connection refused"))
}
