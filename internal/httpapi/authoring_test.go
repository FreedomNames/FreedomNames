package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/routing"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/authoring"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

type authoringDHT struct {
	stubDHT
	mu         sync.Mutex
	current    *record.FNRecord
	resolveErr error
}

func (d *authoringDHT) ResolveRecord(context.Context, string) (*record.FNRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.resolveErr != nil {
		return nil, d.resolveErr
	}
	if d.current == nil {
		return nil, routing.ErrNotFound
	}
	copy := *d.current
	return &copy, nil
}

func (d *authoringDHT) PublishRecord(rec *record.FNRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	copy := *rec
	d.current = &copy
	return nil
}

func newAuthoringHandlers(t *testing.T, initialized bool) (*authoring.Service, *authoringDHT, http.Handler, http.Handler) {
	t.Helper()
	dht := &authoringDHT{stubDHT: stubDHT{initialized: initialized}}
	service, err := authoring.New(t.TempDir(), dht)
	if err != nil {
		t.Fatal(err)
	}
	return service, dht,
		localAuthoringOnly(NamesHandler(service)),
		localAuthoringOnly(NamePublishHandler(service))
}

func requestAuthoring(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:45678"
	req.Host = "localhost:8421"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAuthoringNamesCreateAndList(t *testing.T) {
	_, _, namesHandler, _ := newAuthoringHandlers(t, true)

	rec := requestAuthoring(t, namesHandler, http.MethodGet, "/authoring/names", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"names\":[]}\n" {
		t.Fatalf("empty list: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = requestAuthoring(t, namesHandler, http.MethodPost, "/authoring/names", `{"label":"blog"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created authoring.Name
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Label != "blog" || !strings.HasPrefix(created.Name, "blog.") || !strings.HasSuffix(created.Name, ".fn") {
		t.Fatalf("created = %+v", created)
	}
	// The response contract contains public metadata only.
	var fields map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields["label"] != created.Label || fields["name"] != created.Name {
		t.Fatalf("unexpected create response fields: %v", fields)
	}

	rec = requestAuthoring(t, namesHandler, http.MethodGet, "/authoring/names", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), created.Name) {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = requestAuthoring(t, namesHandler, http.MethodPost, "/authoring/names", `{"label":"blog"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthoringPublishBuildsSignedRecord(t *testing.T) {
	service, dht, _, publishHandler := newAuthoringHandlers(t, true)
	name, err := service.CreateName("blog")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"records":[{"type":"A","value":"10.0.0.5","ttl":300}]}`
	rec := requestAuthoring(t, publishHandler, http.MethodPost, "/authoring/names/blog/publish", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Published string `json:"published"`
		Seq       uint64 `json:"seq"`
		Expires   int64  `json:"expires"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Published != name.Name || response.Seq == 0 || response.Expires == 0 {
		t.Fatalf("response = %+v, name=%+v", response, name)
	}
	dht.mu.Lock()
	published := dht.current
	dht.mu.Unlock()
	if published == nil || published.Seq != response.Seq {
		t.Fatalf("DHT record = %+v, response=%+v", published, response)
	}
	if err := published.Verify(); err != nil {
		t.Fatalf("published record does not verify: %v", err)
	}

	// A second publication within the same second must still supersede it.
	rec = requestAuthoring(t, publishHandler, http.MethodPost, "/authoring/names/blog/publish", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("republish: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var second struct {
		Seq uint64 `json:"seq"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.Seq != response.Seq+1 {
		t.Fatalf("second seq=%d, want %d", second.Seq, response.Seq+1)
	}
}

func TestAuthoringErrorsAreStructured(t *testing.T) {
	service, _, namesHandler, publishHandler := newAuthoringHandlers(t, true)
	if _, err := service.CreateName("blog"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		status  int
	}{
		{"bad label", namesHandler, http.MethodPost, "/authoring/names", `{"label":"../escape"}`, http.StatusBadRequest},
		{"unknown field", namesHandler, http.MethodPost, "/authoring/names", `{"label":"new","privateKey":"steal"}`, http.StatusBadRequest},
		{"trailing JSON", namesHandler, http.MethodPost, "/authoring/names", `{"label":"new"}{}`, http.StatusBadRequest},
		{"wrong list method", namesHandler, http.MethodDelete, "/authoring/names", "", http.StatusMethodNotAllowed},
		{"missing key", publishHandler, http.MethodPost, "/authoring/names/missing/publish", `{"records":[{"type":"A","value":"10.0.0.5","ttl":300}]}`, http.StatusNotFound},
		{"empty records", publishHandler, http.MethodPost, "/authoring/names/blog/publish", `{"records":[]}`, http.StatusBadRequest},
		{"bad record", publishHandler, http.MethodPost, "/authoring/names/blog/publish", `{"records":[{"type":"A","value":"not-an-ip","ttl":300}]}`, http.StatusBadRequest},
		{"nested path", publishHandler, http.MethodPost, "/authoring/names/a/b/publish", `{"records":[]}`, http.StatusBadRequest},
		{"wrong publish method", publishHandler, http.MethodGet, "/authoring/names/blog/publish", "", http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := requestAuthoring(t, tc.handler, tc.method, tc.path, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Fatalf("unstructured error body %q: %v", rec.Body.String(), err)
			}
		})
	}
}

func TestAuthoringPublishRequiresReadyNode(t *testing.T) {
	service, _, _, publishHandler := newAuthoringHandlers(t, false)
	if _, err := service.CreateName("blog"); err != nil {
		t.Fatal(err)
	}
	rec := requestAuthoring(t, publishHandler, http.MethodPost, "/authoring/names/blog/publish", `{"records":[{"type":"A","value":"10.0.0.5","ttl":300}]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthoringPublishRefusesUnknownCurrentSequence(t *testing.T) {
	service, dht, _, publishHandler := newAuthoringHandlers(t, true)
	if _, err := service.CreateName("blog"); err != nil {
		t.Fatal(err)
	}
	dht.resolveErr = context.DeadlineExceeded
	rec := requestAuthoring(t, publishHandler, http.MethodPost, "/authoring/names/blog/publish", `{"records":[{"type":"A","value":"10.0.0.5","ttl":300}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	dht.mu.Lock()
	published := dht.current
	dht.mu.Unlock()
	if published != nil {
		t.Fatalf("published despite unresolved current sequence: %+v", published)
	}
}

func TestAuthoringAPIIsLoopbackOnly(t *testing.T) {
	_, _, namesHandler, _ := newAuthoringHandlers(t, true)
	cases := []struct {
		name      string
		remote    string
		host      string
		forwarded string
		want      int
	}{
		{"IPv4 loopback", "127.0.0.1:1234", "localhost:8421", "", http.StatusOK},
		{"IPv6 loopback", "[::1]:1234", "[::1]:8421", "", http.StatusOK},
		{"LAN peer", "192.168.1.20:1234", "localhost:8421", "", http.StatusForbidden},
		{"public peer", "203.0.113.8:1234", "localhost:8421", "", http.StatusForbidden},
		{"malformed peer", "malformed", "localhost:8421", "", http.StatusForbidden},
		{"LAN host through local proxy", "127.0.0.1:1234", "192.168.1.10:8421", "", http.StatusForbidden},
		{"named host through local proxy", "127.0.0.1:1234", "node.example:8421", "", http.StatusForbidden},
		{"forwarded request", "127.0.0.1:1234", "localhost:8421", "203.0.113.8", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/authoring/names", nil)
			req.RemoteAddr = tc.remote
			req.Host = tc.host
			if tc.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			rec := httptest.NewRecorder()
			namesHandler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestAuthoringOriginDiffersFromContentAPI(t *testing.T) {
	_, _, namesHandler, _ := newAuthoringHandlers(t, true)
	handler := localAPIGuard(namesHandler, nil)
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8421/authoring/names", strings.NewReader(`{"label":"driveby"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Host = "localhost:8421"
	req.Header.Set("Origin", "http://localhost:8420")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("content-origin request status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthoringRejectsNoCORSReadFromContentOrigin(t *testing.T) {
	_, _, namesHandler, _ := newAuthoringHandlers(t, true)
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8421/authoring/names", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Host = "localhost:8421"
	// A no-cors subresource GET has no Origin, but Fetch Metadata still tells
	// us it came from content on the sibling 8420 origin.
	req.Header.Set("Sec-Fetch-Site", "same-site")
	rec := httptest.NewRecorder()
	namesHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("same-site no-cors read status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListenAuthoringRejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":8421", "192.168.1.10:0", "node.example:8421"} {
		if listener, err := listenAuthoring(addr); err == nil {
			listener.Close()
			t.Errorf("listenAuthoring(%q) unexpectedly succeeded", addr)
		}
	}
	listener, err := listenAuthoring("127.0.0.1:0")
	if err != nil {
		t.Fatalf("loopback listen: %v", err)
	}
	listener.Close()
}
