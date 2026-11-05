# httpjson

Small helpers for JSON APIs built on `net/http`. No dependencies.

```sh
go get github.com/ChrisWaldenDev/httpjson
```

## Usage

A handler that creates an item from a JSON body like `{"name": "Hammer"}`:

```go
type createItemRequest struct {
	Name string `json:"name"`
}

func createItem(w http.ResponseWriter, r *http.Request) {
	var req createItemRequest
	if err := httpjson.Decode(w, r, &req); err != nil {
		httpjson.WriteError(w, err) // already a 400 or 413
		return
	}
	if req.Name == "" {
		httpjson.WriteError(w, httpjson.Errorf(http.StatusBadRequest, "name is required"))
		return
	}
	item, err := db.CreateItem(req.Name)
	if err != nil {
		httpjson.WriteError(w, err) // logged, client sees a 500
		return
	}
	httpjson.Write(w, http.StatusCreated, item)
}
```

## API

| Name | What it is |
| --- | --- |
| [`Decode(w, r, &v)`](#decode) | Reads a JSON request body into `v`. |
| [`Write(w, status, v)`](#write) | Sends `v` as a JSON response. |
| [`WriteError(w, err)`](#writeerror) | Sends an error as a JSON error response. |
| [`Errorf(status, format, args...)`](#errorf) | Makes an error that's safe to show the client. |
| [`Error`](#error) | The error type `Errorf` and `Decode` return. |
| [`MaxBodyBytes`](#maxbodybytes) | The request body size limit used by `Decode`. |

### Decode

```go
func Decode(w http.ResponseWriter, r *http.Request, v any) error
```

Reads the request body into `v`, which should be a pointer. Any error it
returns is an `*Error`, so pass it straight to `WriteError`:

| Request body | Status | Message |
| --- | --- | --- |
| Empty | 400 | `request body is empty` |
| Not valid JSON | 400 | `invalid JSON body` |
| A field has the wrong type | 400 | `field "name" must be a string` |
| A nested field has the wrong type | 400 | `field "tags.0.label" must be a string` |
| A number has a fraction but the field is an integer | 400 | `field "id" must be an integer` |
| A number is too big or small for its field | 400 | `field "id" is out of range` |
| The whole body is the wrong type, like `[]` for a struct | 400 | `request body must be an object` |
| Has a field `v` doesn't have | 400 | `unknown field "title"` |
| Has more data after the JSON value | 400 | `request body must contain a single JSON value` |
| Larger than `MaxBodyBytes` | 413 | `request body too large` |

Field names in messages are the JSON names from your struct tags, so they
match what the client sent.

```go
var req createItemRequest
if err := httpjson.Decode(w, r, &req); err != nil {
	httpjson.WriteError(w, err)
	return
}
```

### Write

```go
func Write(w http.ResponseWriter, status int, v any)
```

Encodes `v` as JSON and sends it with the given status and
`Content-Type: application/json`. If `v` can't be encoded, a 500 is sent
instead.

```go
httpjson.Write(w, http.StatusOK, item)
httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
```

### WriteError

```go
func WriteError(w http.ResponseWriter, err error)
```

Sends `err` as `{"error": "..."}`:

- An `*Error`, or an error that wraps one, is sent with its status and message.
- Any other error is logged and sent as a 500 with the body
  `{"error":"internal server error"}`, so internal details like database
  errors never reach the client.

Errors are logged with `slog.Error` on slog's default logger. To change the
format or destination, set your own logger when your app starts:

```go
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
```

```go
httpjson.WriteError(w, httpjson.Errorf(http.StatusNotFound, "item %d not found", id)) // 404
httpjson.WriteError(w, errors.New("database connection refused"))                    // 500
```

### Errorf

```go
func Errorf(status int, format string, args ...any) error
```

Makes an `*Error` with a `fmt.Sprintf`-style message. Use it for anything the
client should see: validation failures, bad path values, and your own errors
like "not found". The status must be 4xx or 5xx; anything else is sent as a
500.

To turn your own errors into a status code, check for them in the handler:

```go
item, err := db.FindItem(id)
if errors.Is(err, db.ErrNotFound) {
	httpjson.WriteError(w, httpjson.Errorf(http.StatusNotFound, "item %d not found", id))
	return
}
if err != nil {
	httpjson.WriteError(w, err) // logged, client sees a 500
	return
}
httpjson.Write(w, http.StatusOK, item)
```

### Error

```go
type Error struct {
	Status  int
	Message string
}
```

The error type returned by `Errorf` and `Decode`. You usually only need it
to read the status in tests or middleware:

```go
if e, ok := errors.AsType[*httpjson.Error](err); ok {
	log.Printf("client error %d: %s", e.Status, e.Message)
}
```

### MaxBodyBytes

```go
const MaxBodyBytes = 1 << 20 // 1 MiB
```

The largest request body `Decode` reads. Bigger bodies get a 413.

## Example app

[`example/main.go`](example/main.go) is a small items API using every
function. It keeps items in memory and starts with one, `Hammer`.

```sh
go run ./example
```

| Request | Shows | Response |
| --- | --- | --- |
| `GET /api/health` | `Write` | `200 {"status":"ok"}` |
| `GET /api/items/1` | `Write` | `200 {"id":1,"name":"Hammer"}` |
| `GET /api/items/abc` | `Errorf` for a bad path value | `400 {"error":"item id must be a number"}` |
| `GET /api/items/99` | `Errorf` for a not-found error | `404 {"error":"item 99 not found"}` |
| `POST /api/items` `{"name":"Wrench"}` | `Decode`, `Write` | `201 {"id":2,"name":"Wrench"}` |
| `POST /api/items` `{"name":""}` | `Errorf` for validation | `400 {"error":"name is required"}` |
| `POST /api/items` `{"name":5}` | `Decode` type error | `400 {"error":"field \"name\" must be a string"}` |
| `POST /api/items` `{"title":"Wrench"}` | `Decode` unknown field | `400 {"error":"unknown field \"title\""}` |
| `GET /api/error` | `WriteError` with an unexpected error | `500 {"error":"internal server error"}` |
